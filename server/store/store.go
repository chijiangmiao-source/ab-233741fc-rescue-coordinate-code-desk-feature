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

// SnapshotBoundary 返回当前最新一张坐标卡的签发时间与编号，作为记录查询的
// 快照边界：首批查询确定后，浏览序列即被冻结，之后新签发的卡编号更大，
// 不会插入该序列。表为空时返回空串与 0（边界内没有任何记录）。
func (s *Store) SnapshotBoundary() (snapTime string, snapID int64, err error) {
	err = s.db.QueryRow(`SELECT issued_at, id FROM cards ORDER BY id DESC LIMIT 1`).
		Scan(&snapTime, &snapID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", 0, nil
	case err != nil:
		return "", 0, fmt.Errorf("读取快照边界失败: %w", err)
	}
	return snapTime, snapID, nil
}

// RecordQuery 描述一次记录页查询的边界条件：快照边界冻结浏览序列，
// 键集位置标记翻页进度；时间与编号都成对出现，避免同一秒内签发的
// 多张卡（签发时间精度为秒）在翻页时重复或遗漏。
type RecordQuery struct {
	SnapTime string // 快照边界：只含不晚于该时间签发的记录
	SnapID   int64  // 快照边界：只含编号不超过该值的记录
	LastTime string // 键集位置：上一页最后一行的签发时间（Paged 为真时生效）
	LastID   int64  // 键集位置：上一页最后一行的编号
	Paged    bool   // 是否应用键集位置（首页为假）
	Limit    int    // 返回行数上限（调用方可多取一行判断是否还有更早记录）
}

// ListCards 在快照边界内按签发时间倒序（时间相同按编号倒序）返回坐标卡。
// 边界外（包括查询期间新签发）的记录不会进入结果；键集分页严格推进，
// 保证多页之间不重复、不遗漏。
func (s *Store) ListCards(q RecordQuery) ([]Card, error) {
	where := `id <= ? AND (issued_at < ? OR (issued_at = ? AND id <= ?))`
	args := []any{q.SnapID, q.SnapTime, q.SnapTime, q.SnapID}
	if q.Paged {
		where += ` AND (issued_at < ? OR (issued_at = ? AND id < ?))`
		args = append(args, q.LastTime, q.LastTime, q.LastID)
	}
	rows, err := s.db.Query(
		`SELECT id, x, y, code, issued_at FROM cards WHERE `+where+
			` ORDER BY issued_at DESC, id DESC LIMIT ?`,
		append(args, q.Limit)...,
	)
	if err != nil {
		return nil, fmt.Errorf("查询签发记录失败: %w", err)
	}
	defer rows.Close()

	cards := make([]Card, 0)
	for rows.Next() {
		var card Card
		var issuedAt string
		if err := rows.Scan(&card.ID, &card.X, &card.Y, &card.Code, &issuedAt); err != nil {
			return nil, fmt.Errorf("读取签发记录失败: %w", err)
		}
		card.IssuedAt, err = time.Parse(time.RFC3339, issuedAt)
		if err != nil {
			return nil, fmt.Errorf("解析签发时间失败: %w", err)
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("查询签发记录失败: %w", err)
	}
	return cards, nil
}

// FindByCode 按短码查询已签发的坐标卡；未签发过时 found 为 false。
func (s *Store) FindByCode(code string) (card Card, found bool, err error) {
	row := s.db.QueryRow(`SELECT id, x, y, code, issued_at FROM cards WHERE code = ?`, code)
	var issuedAt string
	switch err = row.Scan(&card.ID, &card.X, &card.Y, &card.Code, &issuedAt); {
	case errors.Is(err, sql.ErrNoRows):
		return Card{}, false, nil
	case err != nil:
		return Card{}, false, fmt.Errorf("查询坐标卡失败: %w", err)
	}
	card.IssuedAt, err = time.Parse(time.RFC3339, issuedAt)
	if err != nil {
		return Card{}, false, fmt.Errorf("解析签发时间失败: %w", err)
	}
	return card, true, nil
}
