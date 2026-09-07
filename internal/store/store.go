package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
  last_viewed_at INTEGER NOT NULL DEFAULT 0,
  last_position INTEGER NOT NULL DEFAULT 0,
  last_viewed_position INTEGER NOT NULL DEFAULT 0,
  external INTEGER NOT NULL DEFAULT 0,
  muted INTEGER NOT NULL DEFAULT 0,
  in_message_box INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY, chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  sender_id TEXT NOT NULL DEFAULT '', sender_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '', type TEXT NOT NULL DEFAULT 'text',
  created_at INTEGER NOT NULL, position INTEGER NOT NULL DEFAULT 0, is_self INTEGER NOT NULL DEFAULT 0,
  mentions_json TEXT NOT NULL DEFAULT '[]', mentions_self INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS person_profiles (
  id TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', avatar TEXT NOT NULL DEFAULT '',
  base TEXT NOT NULL DEFAULT '', department TEXT NOT NULL DEFAULT '',
  checked_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_messages_chat_created ON messages(chat_id, created_at);
CREATE INDEX IF NOT EXISTS idx_chats_last_time ON chats(last_time DESC);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
PRAGMA optimize;`)
	if err != nil {
		return err
	}
	lastViewedAdded, err := s.ensureColumn("chats", "last_viewed_at", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		return err
	}
	if _, err := s.ensureColumn("messages", "mentions_json", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if _, err := s.ensureColumn("messages", "mentions_self", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	lastPositionAdded, err := s.ensureColumn("chats", "last_position", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		return err
	}
	lastViewedPositionAdded, err := s.ensureColumn("chats", "last_viewed_position", "INTEGER NOT NULL DEFAULT 0")
	if err != nil {
		return err
	}
	if _, err := s.ensureColumn("messages", "position", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	baseAdded, err := s.ensureColumn("person_profiles", "base", "TEXT NOT NULL DEFAULT ''")
	if err != nil {
		return err
	}
	departmentAdded, err := s.ensureColumn("person_profiles", "department", "TEXT NOT NULL DEFAULT ''")
	if err != nil {
		return err
	}
	if baseAdded || departmentAdded {
		// Existing avatar-only cache rows need one refresh to populate the new
		// optional directory fields. Subsequent empty results retain the normal
		// 24-hour cache and do not create a retry loop for confidential users.
		if _, err := s.db.Exec(`UPDATE person_profiles SET checked_at=0`); err != nil {
			return err
		}
	}
	if _, err := s.ensureColumn("chats", "muted", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if _, err := s.ensureColumn("chats", "in_message_box", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if lastViewedAdded {
		// Existing history predates local activity tracking and must not suddenly
		// appear as thousands of new messages after an upgrade.
		if _, err := s.db.Exec(`UPDATE chats SET last_viewed_at=last_time`); err != nil {
			return err
		}
	}
	if lastPositionAdded || lastViewedPositionAdded {
		if _, err := s.db.Exec(`UPDATE chats SET last_viewed_position=last_position WHERE last_viewed_at>=last_time`); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_chat_order ON messages(chat_id,created_at,position)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_messages_chat_mention_created ON messages(chat_id,mentions_self,created_at)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_chats_message_box_last_time ON chats(in_message_box,last_time DESC)`); err != nil {
		return err
	}
	if err := s.migrateKnowledge(); err != nil {
		return err
	}
	if err := s.migrateMemory(); err != nil {
		return err
	}
	if err := s.migrateImpression(); err != nil {
		return err
	}
	return s.migrateAgentContext()
}

func (s *Store) ensureColumn(table, name, definition string) (bool, error) {
	if table != "chats" && table != "messages" && table != "person_profiles" {
		return false, fmt.Errorf("unsupported migration table %q", table)
	}
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var column, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		if column == name {
			return false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + name + ` ` + definition)
	return err == nil, err
}

const chatActivitySelect = `SELECT c.id,c.name,c.description,c.type,
 CASE WHEN c.avatar<>'' THEN c.avatar ELSE COALESCE((SELECT p.avatar FROM messages pm JOIN person_profiles p ON p.id=pm.sender_id WHERE pm.chat_id=c.id AND pm.is_self=0 AND p.avatar<>'' ORDER BY pm.created_at DESC,pm.position DESC LIMIT 1),'') END,
 c.last_message,c.last_time,c.last_position,c.external,c.muted,c.in_message_box,
 (SELECT COUNT(*) FROM messages m WHERE m.chat_id=c.id AND (m.created_at>c.last_viewed_at OR (m.created_at=c.last_viewed_at AND m.position>c.last_viewed_position)) AND m.is_self=0),
 (SELECT COUNT(*) FROM messages m WHERE m.chat_id=c.id AND (m.created_at>c.last_viewed_at OR (m.created_at=c.last_viewed_at AND m.position>c.last_viewed_position)) AND m.is_self=0 AND m.mentions_self=1)
 FROM chats c`

func scanChat(scanner interface{ Scan(...any) error }, chat *model.Chat) error {
	return scanner.Scan(&chat.ID, &chat.Name, &chat.Description, &chat.Type, &chat.Avatar, &chat.LastMessage,
		&chat.LastTime, &chat.LastPosition, &chat.External, &chat.Muted, &chat.InMessageBox, &chat.NewMessages, &chat.MentionCount)
}

func (s *Store) Chat(ctx context.Context, id string) (model.Chat, error) {
	var chat model.Chat
	err := scanChat(s.db.QueryRowContext(ctx, chatActivitySelect+` WHERE c.id=?`, id), &chat)
	if err == sql.ErrNoRows {
		return chat, fmt.Errorf("会话不存在")
	}
	return chat, err
}

func (s *Store) UpsertChat(ctx context.Context, c model.Chat) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO chats
 (id,name,description,type,avatar,last_message,last_time,unread,external)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
 name=excluded.name,description=excluded.description,type=excluded.type,
 avatar=excluded.avatar,external=excluded.external`, c.ID, c.Name, c.Description, c.Type, c.Avatar, c.LastMessage, c.LastTime, 0, c.External)
	return err
}

func (s *Store) UpsertMessage(ctx context.Context, m model.Message) error {
	_, err := s.UpsertMessageWithResult(ctx, m)
	return err
}

// UpsertMessageWithResult reports whether this message was first seen locally.
// Metadata repairs (for example message_position backfills) remain idempotent
// and do not look like new chat activity to downstream Agent processors.
func (s *Store) UpsertMessageWithResult(ctx context.Context, m model.Message) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE id=?)`, m.ID).Scan(&exists); err != nil {
		return false, err
	}
	mentions, err := json.Marshal(m.Mentions)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(id,chat_id,sender_id,sender_name,content,type,created_at,position,is_self,mentions_json,mentions_self)
	 VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET sender_id=excluded.sender_id,sender_name=excluded.sender_name,
	 content=excluded.content,type=excluded.type,created_at=excluded.created_at,position=excluded.position,is_self=excluded.is_self,
	 mentions_json=excluded.mentions_json,mentions_self=excluded.mentions_self`,
		m.ID, m.ChatID, m.SenderID, m.SenderName, m.Content, m.Type, m.CreatedAt, m.Position, m.IsSelf, string(mentions), m.MentionsSelf)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE chats SET
	 last_message=CASE WHEN ? > last_time OR (?=last_time AND ?>last_position) THEN ? ELSE last_message END,
	 last_position=CASE WHEN ? > last_time OR (?=last_time AND ?>last_position) THEN ? ELSE last_position END,
	 last_time=MAX(last_time, ?) WHERE id=?`, m.CreatedAt, m.CreatedAt, m.Position, m.Content,
		m.CreatedAt, m.CreatedAt, m.Position, m.Position, m.CreatedAt, m.ChatID)
	if err != nil {
		return false, err
	}
	if exists != 0 {
		// A position backfill must not resurrect already-viewed historical
		// messages as unread merely because the old schema lacked the tie-breaker.
		if _, err := tx.ExecContext(ctx, `UPDATE chats SET last_viewed_position=last_position
		 WHERE id=? AND last_viewed_at>=last_time`, m.ChatID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return exists == 0, nil
}

// ReclassifyMessages repairs local sender/mention flags after the authenticated
// lark-cli identity changes or its auth-status response shape is upgraded.
func (s *Store) ReclassifyMessages(ctx context.Context, selfID string) error {
	if selfID == "" {
		return fmt.Errorf("当前用户 ID 不能为空")
	}
	type classification struct {
		id           string
		isSelf       bool
		mentionsSelf bool
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,sender_id,mentions_json FROM messages`)
	if err != nil {
		return err
	}
	var updates []classification
	for rows.Next() {
		var id, senderID, mentionsJSON string
		if err := rows.Scan(&id, &senderID, &mentionsJSON); err != nil {
			rows.Close()
			return err
		}
		var mentions []model.Mention
		if err := json.Unmarshal([]byte(mentionsJSON), &mentions); err != nil {
			rows.Close()
			return fmt.Errorf("解析消息 %s 的 @ 信息: %w", id, err)
		}
		mentionsSelf := false
		for _, mention := range mentions {
			if mention.ID == selfID {
				mentionsSelf = true
				break
			}
		}
		updates = append(updates, classification{id: id, isSelf: senderID == selfID, mentionsSelf: mentionsSelf})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statement, err := tx.PrepareContext(ctx, `UPDATE messages SET is_self=?,mentions_self=? WHERE id=?`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, update := range updates {
		if _, err := statement.ExecContext(ctx, update.isSelf, update.mentionsSelf, update.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Chats(ctx context.Context) ([]model.Chat, error) {
	rows, err := s.db.QueryContext(ctx, chatActivitySelect+` ORDER BY c.last_time DESC,c.last_position DESC,c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Chat
	for rows.Next() {
		var c model.Chat
		if err := scanChat(rows, &c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ChatsNeedingMessagePositions limits a resumable CLI backfill to chats that
// still contain recent rows from the pre-position schema.
func (s *Store) ChatsNeedingMessagePositions(ctx context.Context, since int64) ([]model.Chat, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.name,c.type FROM chats c
	 WHERE EXISTS(SELECT 1 FROM messages m WHERE m.chat_id=c.id AND m.created_at>=? AND m.position=0)
	 ORDER BY c.last_time DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chats []model.Chat
	for rows.Next() {
		var chat model.Chat
		if err := rows.Scan(&chat.ID, &chat.Name, &chat.Type); err != nil {
			return nil, err
		}
		chats = append(chats, chat)
	}
	return chats, rows.Err()
}

// MarkChatViewed advances only this application's local activity watermark.
// It does not mutate Feishu's official read state.
func (s *Store) MarkChatViewed(ctx context.Context, chatID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE chats SET last_viewed_at=last_time,last_viewed_position=last_position,unread=0 WHERE id=?`, chatID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) MessageContainsResource(ctx context.Context, messageID, resourceKey string) (bool, error) {
	var content string
	if err := s.db.QueryRowContext(ctx, `SELECT content FROM messages WHERE id=?`, messageID).Scan(&content); err != nil {
		return false, err
	}
	return strings.Contains(content, resourceKey), nil
}

// MarkAllChatsViewed advances this application's local activity watermark for
// every conversation. It does not mutate Feishu's official read state.
func (s *Store) MarkAllChatsViewed(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE chats SET last_viewed_at=last_time,last_viewed_position=last_position,unread=0 WHERE last_viewed_at<last_time OR (last_viewed_at=last_time AND last_viewed_position<last_position) OR unread<>0`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetChatPreferences persists local inbox preferences for a group. Syncing a
// chat later intentionally leaves these user-owned fields untouched.
func (s *Store) SetChatPreferences(ctx context.Context, chatID string, muted, inMessageBox *bool) error {
	if muted == nil && inMessageBox == nil {
		return fmt.Errorf("至少需要设置一个会话状态")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var chatType string
	var currentMuted, currentInMessageBox bool
	if err := tx.QueryRowContext(ctx, `SELECT type,muted,in_message_box FROM chats WHERE id=?`, chatID).Scan(&chatType, &currentMuted, &currentInMessageBox); err != nil {
		return err
	}
	if chatType != "group" {
		return fmt.Errorf("只有群聊支持屏蔽和消息盒子")
	}
	if muted != nil {
		currentMuted = *muted
	}
	if inMessageBox != nil {
		currentInMessageBox = *inMessageBox
	}
	if _, err := tx.ExecContext(ctx, `UPDATE chats SET muted=?,in_message_box=? WHERE id=?`, currentMuted, currentInMessageBox, chatID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) HasMessagesWithoutTimestamp(ctx context.Context) bool {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE created_at=0 LIMIT 1)`).Scan(&exists); err != nil {
		return false
	}
	return exists == 1
}

func (s *Store) MessageIDsWithoutTimestamp(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM messages WHERE created_at=0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RepairMessageTimestamps updates only missing timestamps, preserving the
// locally stored message body and sender fields. Chat previews are then rebuilt
// from the repaired chronology.
func (s *Store) RepairMessageTimestamps(ctx context.Context, messages []model.Message) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var repaired int64
	for _, message := range messages {
		if message.ID == "" || message.CreatedAt <= 0 {
			continue
		}
		result, err := tx.ExecContext(ctx, `UPDATE messages SET created_at=? WHERE id=? AND created_at=0`, message.CreatedAt, message.ID)
		if err != nil {
			return 0, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		repaired += count
	}
	if repaired > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE chats SET
	 last_time=COALESCE((SELECT created_at FROM messages WHERE chat_id=chats.id ORDER BY created_at DESC,position DESC,id DESC LIMIT 1),0),
	 last_position=COALESCE((SELECT position FROM messages WHERE chat_id=chats.id ORDER BY created_at DESC,position DESC,id DESC LIMIT 1),0),
	 last_message=COALESCE((SELECT content FROM messages WHERE chat_id=chats.id ORDER BY created_at DESC,position DESC,id DESC LIMIT 1),'')
 WHERE EXISTS(SELECT 1 FROM messages WHERE chat_id=chats.id)`)
		if err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return repaired, nil
}

func (s *Store) Messages(ctx context.Context, chatID string, limit int) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.chat_id,m.sender_id,m.sender_name,COALESCE(p.avatar,''),m.content,m.type,m.created_at,m.position,m.is_self,m.mentions_json,m.mentions_self FROM
	 (SELECT id,chat_id,sender_id,sender_name,content,type,created_at,position,is_self,mentions_json,mentions_self FROM messages WHERE chat_id=? ORDER BY created_at DESC,position DESC,id DESC LIMIT ?) m
	 LEFT JOIN person_profiles p ON p.id=m.sender_id ORDER BY m.created_at ASC,m.position ASC,m.id ASC`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Message
	for rows.Next() {
		var m model.Message
		var mentions string
		if err := rows.Scan(&m.ID, &m.ChatID, &m.SenderID, &m.SenderName, &m.SenderAvatar, &m.Content, &m.Type, &m.CreatedAt, &m.Position, &m.IsSelf, &mentions, &m.MentionsSelf); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(mentions), &m.Mentions)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MessagesSince(ctx context.Context, chatID string, since int64, limit int) ([]model.Message, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id,m.chat_id,m.sender_id,m.sender_name,COALESCE(p.avatar,''),m.content,m.type,m.created_at,m.position,m.is_self,m.mentions_json,m.mentions_self FROM
	 (SELECT id,chat_id,sender_id,sender_name,content,type,created_at,position,is_self,mentions_json,mentions_self FROM messages
	  WHERE chat_id=? AND created_at>=? ORDER BY created_at DESC,position DESC,id DESC LIMIT ?)
	 m LEFT JOIN person_profiles p ON p.id=m.sender_id ORDER BY m.created_at ASC,m.position ASC,m.id ASC`, chatID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Message
	for rows.Next() {
		var message model.Message
		var mentions string
		if err := rows.Scan(&message.ID, &message.ChatID, &message.SenderID, &message.SenderName, &message.SenderAvatar, &message.Content, &message.Type, &message.CreatedAt, &message.Position, &message.IsSelf, &mentions, &message.MentionsSelf); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(mentions), &message.Mentions)
		out = append(out, message)
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

func (s *Store) DeleteJSON(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, key)
	return err
}
