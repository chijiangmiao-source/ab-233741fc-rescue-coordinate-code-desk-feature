// Package store 用 SQLite 保存成功签发的坐标卡。
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Card 是一张成功签发的坐标卡。
type Card struct {
	ID       int64     `json:"id"`
	X        int       `json:"x"`
	Y        int       `json:"y"`
	Code     string    `json:"code"`
	IssuedAt time.Time `json:"issued_at"`
}

// Store 封装坐标卡的持久化。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）SQLite 数据库并建表。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS cards (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    x         INTEGER NOT NULL,
    y         INTEGER NOT NULL,
    code      TEXT    NOT NULL UNIQUE,
    issued_at TEXT    NOT NULL
);`); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// SaveCard 把成功签发的坐标卡落库；同一短码重复签发时返回首次签发的记录，
// 保证一张短码在全库只对应唯一坐标与唯一签发时间。
func (s *Store) SaveCard(x, y int, code string) (Card, error) {
	now := time.Now().UTC()
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO cards (x, y, code, issued_at) VALUES (?, ?, ?, ?)`,
		x, y, code, now.Format(time.RFC3339),
	); err != nil {
		return Card{}, fmt.Errorf("坐标卡落库失败: %w", err)
	}
	card, _, err := s.FindByCode(code)
	return card, err
}

// FindByCode 按短码查询已签发的坐标卡；未签发过时 found 为 false。
func (s *Store) FindByCode(code string) (card Card, found bool, err error) {
	row := s.db.QueryRow(`SELECT id, x, y, code, issued_at FROM cards WHERE code = ?`, code)
	card, err = scanCard(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, false, nil
	case err != nil:
		return Card{}, false, fmt.Errorf("查询坐标卡失败: %w", err)
	}
	return card, true, nil
}

// CardAt 按主键查询坐标卡，用于校验分页游标所引用的快照边界与键集位置。
func (s *Store) CardAt(id int64) (card Card, found bool, err error) {
	row := s.db.QueryRow(`SELECT id, x, y, code, issued_at FROM cards WHERE id = ?`, id)
	card, err = scanCard(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, false, nil
	case err != nil:
		return Card{}, false, fmt.Errorf("查询坐标卡失败: %w", err)
	}
	return card, true, nil
}

// Snapshot 是一次签发记录浏览的快照边界：首批查询那一刻最新一张卡的
// 编号与签发时间。后续翻页只返回 id <= BoundaryID 的卡，浏览期间新签发
// 的卡（编号必然更大）不会插入当前序列，既不重复也不遗漏。
type Snapshot struct {
	BoundaryID int64
	IssuedAt   string // 与库中一致的 RFC3339 文本
}

// cardColumns 是坐标卡列表查询的固定列，顺序必须与 scanCard 一致。
const cardColumns = `SELECT id, x, y, code, issued_at FROM cards`

// PageFirst 取首批记录：以查询瞬间最新一张卡确定快照边界，按签发时间倒序
// （同一秒内以编号倒序兜底）返回至多 limit 张卡。多取一条用于判断是否
// 还有更早记录。库为空时 cards 为 nil、hasMore 为 false、快照为零值。
func (s *Store) PageFirst(limit int) (cards []Card, snapshot Snapshot, hasMore bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, Snapshot{}, false, fmt.Errorf("开启查询事务失败: %w", err)
	}
	defer tx.Rollback()

	row := tx.QueryRow(`SELECT id, issued_at FROM cards ORDER BY id DESC LIMIT 1`)
	switch err = row.Scan(&snapshot.BoundaryID, &snapshot.IssuedAt); {
	case errors.Is(err, sql.ErrNoRows):
		// 返回非空切片，保证 JSON 序列化为 [] 而不是 null。
		return []Card{}, Snapshot{}, false, nil
	case err != nil:
		return nil, Snapshot{}, false, fmt.Errorf("确定快照边界失败: %w", err)
	}

	rows, err := tx.Query(
		cardColumns+` WHERE id <= ? ORDER BY issued_at DESC, id DESC LIMIT ?`,
		snapshot.BoundaryID, limit+1,
	)
	if err != nil {
		return nil, Snapshot{}, false, fmt.Errorf("查询首批记录失败: %w", err)
	}
	cards, hasMore, err = collectPage(rows, limit)
	if err != nil {
		return nil, Snapshot{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, Snapshot{}, false, fmt.Errorf("提交查询事务失败: %w", err)
	}
	return cards, snapshot, hasMore, nil
}

// PageAfter 在指定快照内取键集下一页：返回签发时间早于 (afterIssuedAt,
// afterID) 的卡。键集比较用 (issued_at, id) 二元组，同一秒签发的卡也能
// 稳定翻页；id <= snapshotID 把序列钉死在首批查询的快照上。
func (s *Store) PageAfter(snapshotID int64, afterIssuedAt string, afterID int64, limit int) ([]Card, bool, error) {
	rows, err := s.db.Query(
		cardColumns+` WHERE id <= ?
            AND (issued_at < ? OR (issued_at = ? AND id < ?))
            ORDER BY issued_at DESC, id DESC LIMIT ?`,
		snapshotID, afterIssuedAt, afterIssuedAt, afterID, limit+1,
	)
	if err != nil {
		return nil, false, fmt.Errorf("查询下一页记录失败: %w", err)
	}
	return collectPage(rows, limit)
}

// collectPage 扫描一页：至多保留 limit 条，多取到的一条只用于 hasMore。
func collectPage(rows *sql.Rows, limit int) ([]Card, bool, error) {
	defer rows.Close()
	cards := make([]Card, 0, limit)
	for rows.Next() {
		card, err := scanCard(rows)
		if err != nil {
			return nil, false, err
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("遍历坐标卡失败: %w", err)
	}
	hasMore := len(cards) > limit
	if hasMore {
		cards = cards[:limit]
	}
	return cards, hasMore, nil
}

// rowScanner 同时兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

// scanCard 扫描一行坐标卡并解析签发时间。
func scanCard(scanner rowScanner) (Card, error) {
	var card Card
	var issuedAt string
	if err := scanner.Scan(&card.ID, &card.X, &card.Y, &card.Code, &issuedAt); err != nil {
		return Card{}, err
	}
	t, err := time.Parse(time.RFC3339, issuedAt)
	if err != nil {
		return Card{}, fmt.Errorf("解析签发时间失败: %w", err)
	}
	card.IssuedAt = t
	return card, nil
}
