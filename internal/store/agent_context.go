package store

import (
	"context"
	"encoding/json"

	"github.com/youdisn/lark-ob/internal/model"
)

func (s *Store) migrateAgentContext() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS agent_chat_contexts (
  chat_id TEXT PRIMARY KEY REFERENCES chats(id) ON DELETE CASCADE,
  summary TEXT NOT NULL DEFAULT '',
  topics_json TEXT NOT NULL DEFAULT '[]',
  participants_json TEXT NOT NULL DEFAULT '[]',
  reply_target_message_id TEXT NOT NULL DEFAULT '',
  source_last_time INTEGER NOT NULL DEFAULT 0,
  source_last_position INTEGER NOT NULL DEFAULT 0,
  model TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_agent_chat_contexts_updated ON agent_chat_contexts(updated_at DESC);
PRAGMA optimize;`)
	return err
}

func (s *Store) SaveAgentChatContext(ctx context.Context, value model.AgentChatContext) error {
	topics, err := json.Marshal(value.Topics)
	if err != nil {
		return err
	}
	participants, err := json.Marshal(value.Participants)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_chat_contexts
 (chat_id,summary,topics_json,participants_json,reply_target_message_id,source_last_time,source_last_position,model,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(chat_id) DO UPDATE SET
 summary=excluded.summary,topics_json=excluded.topics_json,participants_json=excluded.participants_json,
 reply_target_message_id=excluded.reply_target_message_id,source_last_time=excluded.source_last_time,
 source_last_position=excluded.source_last_position,model=excluded.model,updated_at=excluded.updated_at`,
		value.ChatID, value.Summary, string(topics), string(participants), value.ReplyTargetMessageID,
		value.SourceLastTime, value.SourceLastPosition, value.Model, value.UpdatedAt)
	return err
}

func (s *Store) GetAgentChatContext(ctx context.Context, chatID string) (model.AgentChatContext, error) {
	var value model.AgentChatContext
	var topics, participants string
	err := s.db.QueryRowContext(ctx, `SELECT chat_id,summary,topics_json,participants_json,reply_target_message_id,
 source_last_time,source_last_position,model,updated_at FROM agent_chat_contexts WHERE chat_id=?`, chatID).
		Scan(&value.ChatID, &value.Summary, &topics, &participants, &value.ReplyTargetMessageID,
			&value.SourceLastTime, &value.SourceLastPosition, &value.Model, &value.UpdatedAt)
	if err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(topics), &value.Topics); err != nil {
		return value, err
	}
	if err := json.Unmarshal([]byte(participants), &value.Participants); err != nil {
		return value, err
	}
	return value, nil
}
