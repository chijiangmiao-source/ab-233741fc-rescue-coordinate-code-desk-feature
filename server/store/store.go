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
