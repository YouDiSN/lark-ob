package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/youdisn/lark-ob/internal/model"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS chats (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
  type TEXT NOT NULL, avatar TEXT NOT NULL DEFAULT '', last_message TEXT NOT NULL DEFAULT '',
  last_time INTEGER NOT NULL DEFAULT 0, unread INTEGER NOT NULL DEFAULT 0,
  external INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY, chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  sender_id TEXT NOT NULL DEFAULT '', sender_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '', type TEXT NOT NULL DEFAULT 'text',
  created_at INTEGER NOT NULL, is_self INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_created ON messages(chat_id, created_at);
CREATE INDEX IF NOT EXISTS idx_chats_last_time ON chats(last_time DESC);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
PRAGMA optimize;`)
	return err
}

func (s *Store) UpsertChat(ctx context.Context, c model.Chat) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chats
 (id,name,description,type,avatar,last_message,last_time,unread,external)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
 name=excluded.name,description=excluded.description,type=excluded.type,
 avatar=excluded.avatar,external=excluded.external`, c.ID, c.Name, c.Description, c.Type, c.Avatar, c.LastMessage, c.LastTime, c.Unread, c.External)
	return err
}

func (s *Store) UpsertMessage(ctx context.Context, m model.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(id,chat_id,sender_id,sender_name,content,type,created_at,is_self)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET content=excluded.content,type=excluded.type,sender_name=excluded.sender_name`,
		m.ID, m.ChatID, m.SenderID, m.SenderName, m.Content, m.Type, m.CreatedAt, m.IsSelf)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE chats SET last_message=?, last_time=MAX(last_time, ?) WHERE id=?`, m.Content, m.CreatedAt, m.ChatID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Chats(ctx context.Context) ([]model.Chat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,type,avatar,last_message,last_time,unread,external FROM chats ORDER BY last_time DESC,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Chat
	for rows.Next() {
		var c model.Chat
		if err := rows.Scan(&c.ID, &c.Name, &c.Description, &c.Type, &c.Avatar, &c.LastMessage, &c.LastTime, &c.Unread, &c.External); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Messages(ctx context.Context, chatID string, limit int) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,chat_id,sender_id,sender_name,content,type,created_at,is_self FROM
 (SELECT id,chat_id,sender_id,sender_name,content,type,created_at,is_self FROM messages WHERE chat_id=? ORDER BY created_at DESC LIMIT ?)
 ORDER BY created_at`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.SenderName, &m.Content, &m.Type, &m.CreatedAt, &m.IsSelf); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) SetJSON(ctx context.Context, key string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(b))
	return err
}

func (s *Store) GetJSON(ctx context.Context, key string, value any) error {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&raw); err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), value)
}
