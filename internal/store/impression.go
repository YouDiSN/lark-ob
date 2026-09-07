package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/model"
)

func (s *Store) migrateImpression() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS person_impressions (
  person_id TEXT PRIMARY KEY,
  person_name TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  summary TEXT NOT NULL DEFAULT '',
  communication_guidance TEXT NOT NULL DEFAULT '',
  confidence REAL NOT NULL DEFAULT 0,
  evidence_json TEXT NOT NULL DEFAULT '[]',
  message_count INTEGER NOT NULL DEFAULT 0,
  conversation_count INTEGER NOT NULL DEFAULT 0,
  window_start INTEGER NOT NULL DEFAULT 0,
  window_end INTEGER NOT NULL DEFAULT 0,
  source_last_message_at INTEGER NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1,
  generated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_person_impressions_generated ON person_impressions(generated_at DESC);
CREATE TABLE IF NOT EXISTS person_impression_versions (
  person_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  snapshot_json TEXT NOT NULL,
  generated_at INTEGER NOT NULL,
  PRIMARY KEY(person_id, version)
);
CREATE TABLE IF NOT EXISTS style_profiles (
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  scope_name TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  summary TEXT NOT NULL DEFAULT '',
  guidance TEXT NOT NULL DEFAULT '',
  confidence REAL NOT NULL DEFAULT 0,
  evidence_json TEXT NOT NULL DEFAULT '[]',
  sample_count INTEGER NOT NULL DEFAULT 0,
  window_start INTEGER NOT NULL DEFAULT 0,
  window_end INTEGER NOT NULL DEFAULT 0,
  source_last_message_at INTEGER NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  version INTEGER NOT NULL DEFAULT 1,
  generated_at INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(scope_type, scope_id)
);
CREATE INDEX IF NOT EXISTS idx_style_profiles_generated ON style_profiles(scope_type, generated_at DESC);
CREATE TABLE IF NOT EXISTS style_profile_versions (
  scope_type TEXT NOT NULL,
  scope_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  snapshot_json TEXT NOT NULL,
  generated_at INTEGER NOT NULL,
  PRIMARY KEY(scope_type, scope_id, version)
);
PRAGMA optimize;`)
	return err
}

func (s *Store) PeopleForImpression(ctx context.Context, windowStart, changedSince int64) ([]impression.Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.sender_id,
 COALESCE(MAX(NULLIF(m.sender_name,'')), m.sender_id), COUNT(*), COUNT(DISTINCT m.chat_id), MAX(m.created_at)
 FROM messages m JOIN chats c ON c.id=m.chat_id
 WHERE c.in_message_box=0 AND m.is_self=0 AND m.sender_id<>'' AND m.content<>'' AND m.created_at>=?
 GROUP BY m.sender_id HAVING MAX(m.created_at)>?
 ORDER BY MAX(m.created_at) DESC`, windowStart, changedSince)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var people []impression.Person
	for rows.Next() {
		var person impression.Person
		if err := rows.Scan(&person.ID, &person.Name, &person.MessageCount, &person.ConversationCount, &person.LastMessageAt); err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	return people, rows.Err()
}

func (s *Store) InteractionMessagesSince(ctx context.Context, personID string, since int64, limit int) ([]model.Message, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT q.id,q.chat_id,q.sender_id,q.sender_name,COALESCE(p.avatar,''),q.content,q.type,q.created_at,q.position,q.is_self,q.mentions_json,q.mentions_self
 FROM (
	   SELECT m.id,m.chat_id,m.sender_id,m.sender_name,m.content,m.type,m.created_at,m.position,m.is_self,m.mentions_json,m.mentions_self
   FROM messages m JOIN chats c ON c.id=m.chat_id
   WHERE c.in_message_box=0 AND m.created_at>=? AND (m.sender_id=? OR m.is_self=1)
     AND EXISTS(SELECT 1 FROM messages target WHERE target.chat_id=m.chat_id AND target.sender_id=? AND target.created_at>=?)
	   ORDER BY m.created_at DESC,m.position DESC,m.id DESC LIMIT ?
 ) q LEFT JOIN person_profiles p ON p.id=q.sender_id
	 ORDER BY q.created_at ASC,q.position ASC,q.id ASC`, since, personID, personID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfileMessages(rows)
}

func (s *Store) SelfMessagesSince(ctx context.Context, since int64, limit int) ([]model.Message, error) {
	if limit <= 0 {
		limit = 5000
	}
	rows, err := s.db.QueryContext(ctx, `SELECT q.id,q.chat_id,q.sender_id,q.sender_name,COALESCE(p.avatar,''),q.content,q.type,q.created_at,q.position,q.is_self,q.mentions_json,q.mentions_self
	 FROM (SELECT m.id,m.chat_id,m.sender_id,m.sender_name,m.content,m.type,m.created_at,m.position,m.is_self,m.mentions_json,m.mentions_self
	       FROM messages m JOIN chats c ON c.id=m.chat_id
	       WHERE c.in_message_box=0 AND m.is_self=1 AND m.created_at>=? AND m.content<>''
	       ORDER BY m.created_at DESC,m.position DESC,m.id DESC LIMIT ?) q
	 LEFT JOIN person_profiles p ON p.id=q.sender_id ORDER BY q.created_at ASC,q.position ASC,q.id ASC`, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProfileMessages(rows)
}

type profileMessageRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanProfileMessages(rows profileMessageRows) ([]model.Message, error) {
	var messages []model.Message
	for rows.Next() {
		var message model.Message
		var mentions string
		if err := rows.Scan(&message.ID, &message.ChatID, &message.SenderID, &message.SenderName, &message.SenderAvatar,
			&message.Content, &message.Type, &message.CreatedAt, &message.Position, &message.IsSelf, &mentions, &message.MentionsSelf); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(mentions), &message.Mentions)
		messages = append(messages, message)
	}
	return messages, rows.Err()
}

func (s *Store) SelfMessageStats(ctx context.Context, windowStart, changedSince int64) (int, int64, error) {
	var count int
	var last sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),MAX(m.created_at) FROM messages m JOIN chats c ON c.id=m.chat_id
	 WHERE c.in_message_box=0 AND m.is_self=1 AND m.content<>'' AND m.created_at>=?`, windowStart).Scan(&count, &last)
	return count, last.Int64, err
}

func (s *Store) PersonForChat(ctx context.Context, chatID string) (impression.Person, error) {
	var person impression.Person
	err := s.db.QueryRowContext(ctx, `SELECT m.sender_id,COALESCE(NULLIF(m.sender_name,''),m.sender_id),COUNT(*),1,MAX(m.created_at)
 FROM messages m JOIN chats c ON c.id=m.chat_id
 WHERE m.chat_id=? AND c.type='p2p' AND c.in_message_box=0 AND m.is_self=0 AND m.sender_id<>'' GROUP BY m.sender_id
 ORDER BY MAX(m.created_at) DESC LIMIT 1`, chatID).Scan(&person.ID, &person.Name, &person.MessageCount, &person.ConversationCount, &person.LastMessageAt)
	return person, err
}

func (s *Store) PersonForImpression(ctx context.Context, personID string, windowStart int64) (impression.Person, error) {
	var person impression.Person
	err := s.db.QueryRowContext(ctx, `SELECT m.sender_id,COALESCE(MAX(NULLIF(m.sender_name,'')),m.sender_id),COUNT(*),COUNT(DISTINCT m.chat_id),MAX(m.created_at)
 FROM messages m JOIN chats c ON c.id=m.chat_id
 WHERE c.in_message_box=0 AND m.sender_id=? AND m.is_self=0 AND m.content<>'' AND m.created_at>=? GROUP BY m.sender_id`, personID, windowStart).
		Scan(&person.ID, &person.Name, &person.MessageCount, &person.ConversationCount, &person.LastMessageAt)
	return person, err
}

func (s *Store) SavePersonImpression(ctx context.Context, value impression.PersonImpression) (impression.PersonImpression, error) {
	if value.PersonID == "" {
		return value, fmt.Errorf("人物 ID 不能为空")
	}
	tags, _ := json.Marshal(value.Tags)
	evidence, _ := json.Marshal(value.EvidenceMessageIDs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	var previous int
	_ = tx.QueryRowContext(ctx, `SELECT version FROM person_impressions WHERE person_id=?`, value.PersonID).Scan(&previous)
	value.Version = previous + 1
	snapshot, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO person_impressions
 (person_id,person_name,tags_json,summary,communication_guidance,confidence,evidence_json,message_count,conversation_count,window_start,window_end,source_last_message_at,model,version,generated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(person_id) DO UPDATE SET
 person_name=excluded.person_name,tags_json=excluded.tags_json,summary=excluded.summary,
 communication_guidance=excluded.communication_guidance,confidence=excluded.confidence,evidence_json=excluded.evidence_json,
 message_count=excluded.message_count,conversation_count=excluded.conversation_count,window_start=excluded.window_start,
 window_end=excluded.window_end,source_last_message_at=excluded.source_last_message_at,model=excluded.model,
 version=excluded.version,generated_at=excluded.generated_at`, value.PersonID, value.PersonName, string(tags), value.Summary,
		value.CommunicationGuidance, value.Confidence, string(evidence), value.MessageCount, value.ConversationCount,
		value.WindowStart, value.WindowEnd, value.SourceLastMessageAt, value.Model, value.Version, value.GeneratedAt)
	if err != nil {
		return value, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO person_impression_versions(person_id,version,snapshot_json,generated_at) VALUES(?,?,?,?)`, value.PersonID, value.Version, string(snapshot), value.GeneratedAt); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *Store) GetPersonImpression(ctx context.Context, personID string) (impression.PersonImpression, error) {
	var value impression.PersonImpression
	var tags, evidence string
	err := s.db.QueryRowContext(ctx, `SELECT person_id,person_name,tags_json,summary,communication_guidance,confidence,evidence_json,
 message_count,conversation_count,window_start,window_end,source_last_message_at,model,version,generated_at
 FROM person_impressions WHERE person_id=?`, personID).Scan(&value.PersonID, &value.PersonName, &tags, &value.Summary,
		&value.CommunicationGuidance, &value.Confidence, &evidence, &value.MessageCount, &value.ConversationCount,
		&value.WindowStart, &value.WindowEnd, &value.SourceLastMessageAt, &value.Model, &value.Version, &value.GeneratedAt)
	_ = json.Unmarshal([]byte(tags), &value.Tags)
	_ = json.Unmarshal([]byte(evidence), &value.EvidenceMessageIDs)
	return value, err
}

func (s *Store) SaveStyleProfile(ctx context.Context, value impression.StyleProfile) (impression.StyleProfile, error) {
	if value.ScopeType == "" || value.ScopeID == "" {
		return value, fmt.Errorf("风格范围不能为空")
	}
	tags, _ := json.Marshal(value.Tags)
	evidence, _ := json.Marshal(value.EvidenceMessageIDs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	var previous int
	_ = tx.QueryRowContext(ctx, `SELECT version FROM style_profiles WHERE scope_type=? AND scope_id=?`, value.ScopeType, value.ScopeID).Scan(&previous)
	value.Version = previous + 1
	snapshot, err := json.Marshal(value)
	if err != nil {
		return value, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO style_profiles
 (scope_type,scope_id,scope_name,tags_json,summary,guidance,confidence,evidence_json,sample_count,window_start,window_end,source_last_message_at,model,version,generated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(scope_type,scope_id) DO UPDATE SET
 scope_name=excluded.scope_name,tags_json=excluded.tags_json,summary=excluded.summary,guidance=excluded.guidance,
 confidence=excluded.confidence,evidence_json=excluded.evidence_json,sample_count=excluded.sample_count,
 window_start=excluded.window_start,window_end=excluded.window_end,source_last_message_at=excluded.source_last_message_at,
 model=excluded.model,version=excluded.version,generated_at=excluded.generated_at`, value.ScopeType, value.ScopeID, value.ScopeName,
		string(tags), value.Summary, value.Guidance, value.Confidence, string(evidence), value.SampleCount, value.WindowStart,
		value.WindowEnd, value.SourceLastMessageAt, value.Model, value.Version, value.GeneratedAt)
	if err != nil {
		return value, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO style_profile_versions(scope_type,scope_id,version,snapshot_json,generated_at) VALUES(?,?,?,?,?)`, value.ScopeType, value.ScopeID, value.Version, string(snapshot), value.GeneratedAt); err != nil {
		return value, err
	}
	return value, tx.Commit()
}

func (s *Store) GetStyleProfile(ctx context.Context, scopeType, scopeID string) (impression.StyleProfile, error) {
	var value impression.StyleProfile
	var tags, evidence string
	err := s.db.QueryRowContext(ctx, `SELECT scope_type,scope_id,scope_name,tags_json,summary,guidance,confidence,evidence_json,
 sample_count,window_start,window_end,source_last_message_at,model,version,generated_at
 FROM style_profiles WHERE scope_type=? AND scope_id=?`, scopeType, scopeID).Scan(&value.ScopeType, &value.ScopeID, &value.ScopeName,
		&tags, &value.Summary, &value.Guidance, &value.Confidence, &evidence, &value.SampleCount, &value.WindowStart,
		&value.WindowEnd, &value.SourceLastMessageAt, &value.Model, &value.Version, &value.GeneratedAt)
	_ = json.Unmarshal([]byte(tags), &value.Tags)
	_ = json.Unmarshal([]byte(evidence), &value.EvidenceMessageIDs)
	return value, err
}

// InvalidateDerivedProfilesForChat removes current profiles that may have used
// messages from chatID. Version snapshots remain as audit history, but current
// recommendation reads cannot use stale derived data.
func (s *Store) InvalidateDerivedProfilesForChat(ctx context.Context, chatID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM person_impressions WHERE person_id IN (
 SELECT DISTINCT sender_id FROM messages WHERE chat_id=? AND is_self=0 AND sender_id<>''
)`, chatID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM style_profiles WHERE
 (scope_type='relationship' AND scope_id IN (
   SELECT DISTINCT sender_id FROM messages WHERE chat_id=? AND is_self=0 AND sender_id<>''
 )) OR (scope_type='chat' AND scope_id=?) OR scope_type='self'`, chatID, chatID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key='daily_profile_maintenance'`); err != nil {
		return err
	}
	return tx.Commit()
}
