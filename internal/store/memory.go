package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/memory"
)

func (s *Store) migrateMemory() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS memory_claims (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  subject_name TEXT NOT NULL DEFAULT '',
  chat_id TEXT NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
  category TEXT NOT NULL DEFAULT 'fact',
  content TEXT NOT NULL,
  confidence REAL NOT NULL DEFAULT 0.5,
  evidence_json TEXT NOT NULL DEFAULT '[]',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
	last_evidence_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(subject_type, subject_id, chat_id, category, content)
);
CREATE INDEX IF NOT EXISTS idx_memory_claims_chat ON memory_claims(chat_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_claims_subject ON memory_claims(subject_type, subject_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_claims_type_chat_updated ON memory_claims(subject_type, chat_id, updated_at DESC);
CREATE TABLE IF NOT EXISTS knowledge_summaries (
  source_id INTEGER PRIMARY KEY REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  summary TEXT NOT NULL,
  facts_json TEXT NOT NULL DEFAULT '[]',
  model TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);`)
	if err != nil {
		return err
	}
	if err := s.ensureMemoryColumn("last_evidence_at", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	for name, definition := range map[string]string{
		"source_type": "TEXT NOT NULL DEFAULT 'agent'",
		"importance":  "TEXT NOT NULL DEFAULT 'normal'",
		"valid_from":  "INTEGER NOT NULL DEFAULT 0",
		"valid_until": "INTEGER NOT NULL DEFAULT 0",
		"pinned":      "INTEGER NOT NULL DEFAULT 0",
	} {
		if err := s.ensureMemoryColumn(name, definition); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_memory_claims_active ON memory_claims(source_type,valid_from,valid_until,updated_at DESC)`); err != nil {
		return err
	}
	return s.migrateMemorySearch()
}

func (s *Store) migrateMemorySearch() error {
	_, err := s.db.Exec(`
CREATE VIRTUAL TABLE IF NOT EXISTS memory_claims_fts USING fts5(
  claim_id UNINDEXED,
  subject_name,
  source_chat_name,
  content,
  tokenize='unicode61'
);
CREATE TRIGGER IF NOT EXISTS memory_claims_fts_insert AFTER INSERT ON memory_claims BEGIN
  INSERT INTO memory_claims_fts(claim_id,subject_name,source_chat_name,content)
  VALUES(new.id,new.subject_name,COALESCE((SELECT name FROM chats WHERE id=new.chat_id),''),new.content);
END;
CREATE TRIGGER IF NOT EXISTS memory_claims_fts_delete AFTER DELETE ON memory_claims BEGIN
  DELETE FROM memory_claims_fts WHERE claim_id=old.id;
END;
CREATE TRIGGER IF NOT EXISTS memory_claims_fts_update AFTER UPDATE OF subject_name,chat_id,content ON memory_claims BEGIN
  DELETE FROM memory_claims_fts WHERE claim_id=old.id;
  INSERT INTO memory_claims_fts(claim_id,subject_name,source_chat_name,content)
  VALUES(new.id,new.subject_name,COALESCE((SELECT name FROM chats WHERE id=new.chat_id),''),new.content);
END;
DELETE FROM memory_claims_fts;
INSERT INTO memory_claims_fts(claim_id,subject_name,source_chat_name,content)
SELECT memory_claims.id,memory_claims.subject_name,COALESCE(chats.name,''),memory_claims.content
FROM memory_claims LEFT JOIN chats ON chats.id=memory_claims.chat_id;
`)
	return err
}

func (s *Store) ensureMemoryColumn(name, definition string) error {
	rows, err := s.db.Query(`PRAGMA table_info(memory_claims)`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid int
		var column, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &column, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		if column == name {
			return rows.Close()
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE memory_claims ADD COLUMN ` + name + ` ` + definition)
	return err
}

func (s *Store) ReplaceChatMemoryClaims(ctx context.Context, chatID string, claims []memory.Claim) ([]memory.Claim, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_claims WHERE chat_id=? AND source_type=?`, chatID, memory.SourceAgent); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	saved := make([]memory.Claim, 0, len(claims))
	for i := range claims {
		evidence, marshalErr := json.Marshal(claims[i].EvidenceMessageIDs)
		if marshalErr != nil {
			return nil, marshalErr
		}
		result, insertErr := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memory_claims
 (subject_type,subject_id,subject_name,chat_id,category,content,confidence,evidence_json,created_at,updated_at,last_evidence_at,source_type,importance,valid_from,valid_until,pinned)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, claims[i].SubjectType, claims[i].SubjectID, claims[i].SubjectName, chatID,
			claims[i].Category, claims[i].Content, claims[i].Confidence, string(evidence), now, now, claims[i].LastEvidenceAt,
			memory.SourceAgent, memory.ImportanceNormal, 0, 0, false)
		if insertErr != nil {
			return nil, insertErr
		}
		inserted, insertErr := result.RowsAffected()
		if insertErr != nil {
			return nil, insertErr
		}
		if inserted == 0 {
			// An identical manual fact has higher authority and must survive
			// automatic re-extraction without making the entire chat job fail.
			continue
		}
		claims[i].ID, _ = result.LastInsertId()
		claims[i].ChatID = chatID
		claims[i].CreatedAt = now
		claims[i].UpdatedAt = now
		claims[i].SourceType = memory.SourceAgent
		claims[i].Importance = memory.ImportanceNormal
		saved = append(saved, claims[i])
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return saved, nil
}

func (s *Store) ListChatMemoryClaims(ctx context.Context, chatID string, limit int) ([]memory.Claim, error) {
	rows, err := s.db.QueryContext(ctx, memoryClaimSelect+` WHERE memory_claims.chat_id=? ORDER BY memory_claims.updated_at DESC,memory_claims.id DESC LIMIT ?`, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryClaims(rows)
}

func (s *Store) SearchMemoryContext(ctx context.Context, request memory.ContextRequest) ([]memory.Claim, error) {
	args := []any{request.ChatID}
	clauses := []string{"chat_id=?"}
	if len(request.PersonIDs) > 0 {
		placeholders := make([]string, 0, len(request.PersonIDs))
		for _, id := range request.PersonIDs {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		clauses = append(clauses, "(subject_type='person' AND subject_id IN ("+strings.Join(placeholders, ",")+"))")
	}
	where := "(" + strings.Join(clauses, " OR ") + ")"
	where += " AND (valid_from=0 OR valid_from<=?) AND (valid_until=0 OR valid_until>?)"
	args = append(args, request.ActiveAt, request.ActiveAt)
	if strings.TrimSpace(request.Query) != "" {
		where += " AND content LIKE ?"
		args = append(args, "%"+strings.TrimSpace(request.Query)+"%")
	}
	args = append(args, request.Limit)
	rows, err := s.db.QueryContext(ctx, memoryClaimSelect+` WHERE `+where+` ORDER BY confidence DESC,memory_claims.updated_at DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryClaims(rows)
}

const memoryClaimSelect = `SELECT memory_claims.id,subject_type,subject_id,subject_name,memory_claims.chat_id,category,content,
 confidence,evidence_json,memory_claims.created_at,memory_claims.updated_at,last_evidence_at,COALESCE(chats.name,''),COALESCE(chats.type,''),
 source_type,importance,valid_from,valid_until,pinned
 FROM memory_claims LEFT JOIN chats ON chats.id=memory_claims.chat_id`

func (s *Store) ListMemoryClaims(ctx context.Context, request memory.ListRequest) ([]memory.Claim, error) {
	if query := strings.TrimSpace(request.Query); query != "" {
		ftsQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		claims, err := s.listMemoryClaimsFTS(ctx, request, ftsQuery)
		if err == nil && len(claims) > 0 {
			return claims, nil
		}
	}
	return s.listMemoryClaimsLike(ctx, request)
}

func memoryListClauses(request memory.ListRequest) ([]string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0, 3)
	if request.SubjectType != "" {
		clauses = append(clauses, "subject_type=?")
		args = append(args, request.SubjectType)
	}
	if request.SourceChatType != "" {
		clauses = append(clauses, "chats.type=?")
		args = append(args, request.SourceChatType)
	}
	return clauses, args
}

func (s *Store) listMemoryClaimsFTS(ctx context.Context, request memory.ListRequest, query string) ([]memory.Claim, error) {
	clauses, args := memoryListClauses(request)
	clauses = append(clauses, "memory_claims_fts MATCH ?")
	args = append(args, query, request.Limit, request.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT memory_claims.id,subject_type,subject_id,memory_claims.subject_name,memory_claims.chat_id,category,memory_claims.content,
 confidence,evidence_json,memory_claims.created_at,memory_claims.updated_at,last_evidence_at,COALESCE(chats.name,''),COALESCE(chats.type,''),
 source_type,importance,valid_from,valid_until,pinned
 FROM memory_claims_fts JOIN memory_claims ON memory_claims.id=memory_claims_fts.claim_id
 LEFT JOIN chats ON chats.id=memory_claims.chat_id WHERE `+strings.Join(clauses, " AND ")+`
 ORDER BY bm25(memory_claims_fts),memory_claims.updated_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryClaims(rows)
}

func (s *Store) listMemoryClaimsLike(ctx context.Context, request memory.ListRequest) ([]memory.Claim, error) {
	clauses, args := memoryListClauses(request)
	if query := strings.TrimSpace(request.Query); query != "" {
		clauses = append(clauses, "(content LIKE ? OR subject_name LIKE ? OR chats.name LIKE ?)")
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	}
	args = append(args, request.Limit, request.Offset)
	rows, err := s.db.QueryContext(ctx, memoryClaimSelect+` WHERE `+strings.Join(clauses, " AND ")+` ORDER BY memory_claims.updated_at DESC,memory_claims.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMemoryClaims(rows)
}

func (s *Store) MemoryStats(ctx context.Context) (memory.Stats, error) {
	var stats memory.Stats
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),
 COALESCE(SUM(CASE WHEN subject_type='person' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN subject_type='chat' AND chats.type='group' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN subject_type='chat' AND chats.type='p2p' THEN 1 ELSE 0 END),0),
 COUNT(DISTINCT CASE WHEN subject_type='person' THEN subject_id END),
	COUNT(DISTINCT CASE WHEN subject_type='chat' AND chats.type='group' THEN memory_claims.chat_id END),
	COUNT(DISTINCT CASE WHEN subject_type='chat' AND chats.type='p2p' THEN memory_claims.chat_id END)
	FROM memory_claims LEFT JOIN chats ON chats.id=memory_claims.chat_id`).Scan(
		&stats.Total, &stats.PersonMemories, &stats.GroupMemories, &stats.P2PMemories, &stats.People, &stats.Groups, &stats.P2PChats)
	return stats, err
}

func (s *Store) DeleteMemoryClaim(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM memory_claims WHERE id=?`, id)
	if err != nil {
		return err
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if deleted == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SaveManualMemoryClaim(ctx context.Context, claim memory.Claim) (memory.Claim, error) {
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO memory_claims
 (subject_type,subject_id,subject_name,chat_id,category,content,confidence,evidence_json,created_at,updated_at,last_evidence_at,source_type,importance,valid_from,valid_until,pinned)
	 VALUES(?,?,?,?,?,?,1,'[]',?,?,0,?,?,?,?,?)
	 ON CONFLICT(subject_type,subject_id,chat_id,category,content) DO UPDATE SET
	 subject_name=excluded.subject_name,confidence=1,evidence_json='[]',updated_at=excluded.updated_at,last_evidence_at=0,
	 source_type=excluded.source_type,importance=excluded.importance,valid_from=excluded.valid_from,valid_until=excluded.valid_until,pinned=excluded.pinned`,
		claim.SubjectType, claim.SubjectID, claim.SubjectName, claim.ChatID, claim.Category, claim.Content, now, now,
		memory.SourceManual, claim.Importance, claim.ValidFrom, claim.ValidUntil, claim.Pinned)
	if err != nil {
		return memory.Claim{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT id,created_at FROM memory_claims WHERE subject_type=? AND subject_id=? AND chat_id=? AND category=? AND content=?`,
		claim.SubjectType, claim.SubjectID, claim.ChatID, claim.Category, claim.Content).Scan(&claim.ID, &claim.CreatedAt); err != nil {
		return memory.Claim{}, err
	}
	claim.Confidence, claim.UpdatedAt = 1, now
	claim.SourceType = memory.SourceManual
	return claim, nil
}

func (s *Store) UpdateManualMemoryClaim(ctx context.Context, id int64, claim memory.Claim) (memory.Claim, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE memory_claims SET subject_type=?,subject_id=?,subject_name=?,chat_id=?,category=?,content=?,
 importance=?,valid_from=?,valid_until=?,pinned=?,updated_at=? WHERE id=? AND source_type=?`, claim.SubjectType, claim.SubjectID,
		claim.SubjectName, claim.ChatID, claim.Category, claim.Content, claim.Importance, claim.ValidFrom, claim.ValidUntil, claim.Pinned,
		now, id, memory.SourceManual)
	if err != nil {
		return memory.Claim{}, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return memory.Claim{}, err
	}
	if updated == 0 {
		return memory.Claim{}, sql.ErrNoRows
	}
	claim.ID, claim.Confidence, claim.UpdatedAt, claim.SourceType = id, 1, now, memory.SourceManual
	return claim, nil
}

func (s *Store) MemorySubjectOptions(ctx context.Context) (memory.SubjectOptions, error) {
	var result memory.SubjectOptions
	rows, err := s.db.QueryContext(ctx, `WITH ranked AS (
 SELECT m.sender_id,m.sender_name,m.chat_id,c.name AS chat_name,c.type AS chat_type,
 ROW_NUMBER() OVER(PARTITION BY m.sender_id ORDER BY m.created_at DESC,m.position DESC,m.id DESC) AS rank
 FROM messages m JOIN chats c ON c.id=m.chat_id WHERE m.sender_id<>'' AND m.is_self=0 AND c.in_message_box=0)
 SELECT sender_id,sender_name,chat_id,chat_name,chat_type FROM ranked WHERE rank=1 ORDER BY sender_name`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var option memory.SubjectOption
		option.Type = memory.SubjectPerson
		if err := rows.Scan(&option.ID, &option.Name, &option.ChatID, &option.ChatName, &option.ChatType); err != nil {
			rows.Close()
			return result, err
		}
		result.People = append(result.People, option)
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,name,type FROM chats WHERE in_message_box=0 ORDER BY last_time DESC,last_position DESC,name`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var option memory.SubjectOption
		option.Type = memory.SubjectChat
		if err := rows.Scan(&option.ID, &option.Name, &option.ChatType); err != nil {
			return result, err
		}
		option.ChatID, option.ChatName = option.ID, option.Name
		result.Chats = append(result.Chats, option)
	}
	return result, rows.Err()
}

type memoryRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanMemoryClaims(rows memoryRows) ([]memory.Claim, error) {
	var claims []memory.Claim
	for rows.Next() {
		var claim memory.Claim
		var evidence string
		if err := rows.Scan(&claim.ID, &claim.SubjectType, &claim.SubjectID, &claim.SubjectName, &claim.ChatID,
			&claim.Category, &claim.Content, &claim.Confidence, &evidence, &claim.CreatedAt, &claim.UpdatedAt,
			&claim.LastEvidenceAt, &claim.SourceChatName, &claim.SourceChatType, &claim.SourceType, &claim.Importance,
			&claim.ValidFrom, &claim.ValidUntil, &claim.Pinned); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(evidence), &claim.EvidenceMessageIDs)
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (s *Store) SaveKnowledgeSummary(ctx context.Context, summary knowledge.Summary) (knowledge.Summary, error) {
	facts, err := json.Marshal(summary.Facts)
	if err != nil {
		return summary, err
	}
	now := time.Now().UnixMilli()
	_, err = s.db.ExecContext(ctx, `INSERT INTO knowledge_summaries(source_id,summary,facts_json,model,created_at,updated_at)
 VALUES(?,?,?,?,?,?) ON CONFLICT(source_id) DO UPDATE SET summary=excluded.summary,facts_json=excluded.facts_json,
 model=excluded.model,updated_at=excluded.updated_at`, summary.SourceID, summary.Summary, string(facts), summary.Model, now, now)
	if err != nil {
		return summary, err
	}
	return s.GetKnowledgeSummary(ctx, summary.SourceID)
}

func (s *Store) GetKnowledgeSummary(ctx context.Context, sourceID int64) (knowledge.Summary, error) {
	var summary knowledge.Summary
	var facts string
	err := s.db.QueryRowContext(ctx, `SELECT source_id,summary,facts_json,model,created_at,updated_at FROM knowledge_summaries WHERE source_id=?`, sourceID).
		Scan(&summary.SourceID, &summary.Summary, &facts, &summary.Model, &summary.CreatedAt, &summary.UpdatedAt)
	if err == sql.ErrNoRows {
		return summary, fmt.Errorf("知识源尚未生成摘要")
	}
	if err == nil {
		_ = json.Unmarshal([]byte(facts), &summary.Facts)
	}
	return summary, err
}
