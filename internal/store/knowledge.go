package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/youdisn/lark-ob/internal/knowledge"
)

func (s *Store) migrateKnowledge() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS knowledge_sources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_type TEXT NOT NULL,
  source_url TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  scope_type TEXT NOT NULL DEFAULT 'global',
  scope_id TEXT NOT NULL DEFAULT '',
  document_id TEXT NOT NULL,
  revision_id INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'ready',
  error TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  synced_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_knowledge_sources_scope ON knowledge_sources(scope_type, scope_id);
CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES knowledge_sources(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL,
  heading TEXT NOT NULL DEFAULT '',
  block_id TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  UNIQUE(source_id, ordinal)
);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_source ON knowledge_chunks(source_id, ordinal);
CREATE VIRTUAL TABLE IF NOT EXISTS knowledge_chunks_fts USING fts5(
  chunk_id UNINDEXED,
  source_id UNINDEXED,
  title,
  heading,
  content,
  tokenize='unicode61'
);`)
	return err
}

func (s *Store) ListKnowledgeSources(ctx context.Context) ([]knowledge.Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.source_type,s.source_url,s.title,s.scope_type,s.scope_id,
 s.document_id,s.revision_id,s.status,s.error,s.created_at,s.updated_at,s.synced_at,COUNT(c.id)
 FROM knowledge_sources s LEFT JOIN knowledge_chunks c ON c.source_id=s.id
 GROUP BY s.id ORDER BY s.synced_at DESC,s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []knowledge.Source
	for rows.Next() {
		var source knowledge.Source
		if err := rows.Scan(&source.ID, &source.Type, &source.URL, &source.Title, &source.ScopeType, &source.ScopeID,
			&source.DocumentID, &source.Revision, &source.Status, &source.Error, &source.CreatedAt, &source.UpdatedAt,
			&source.SyncedAt, &source.ChunkCount); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *Store) GetKnowledgeSource(ctx context.Context, id int64) (knowledge.Source, error) {
	var source knowledge.Source
	err := s.db.QueryRowContext(ctx, `SELECT s.id,s.source_type,s.source_url,s.title,s.scope_type,s.scope_id,
 s.document_id,s.revision_id,s.status,s.error,s.created_at,s.updated_at,s.synced_at,COUNT(c.id)
 FROM knowledge_sources s LEFT JOIN knowledge_chunks c ON c.source_id=s.id
 WHERE s.id=? GROUP BY s.id`, id).Scan(
		&source.ID, &source.Type, &source.URL, &source.Title, &source.ScopeType, &source.ScopeID,
		&source.DocumentID, &source.Revision, &source.Status, &source.Error, &source.CreatedAt, &source.UpdatedAt,
		&source.SyncedAt, &source.ChunkCount)
	if err == sql.ErrNoRows {
		return source, fmt.Errorf("知识源不存在")
	}
	return source, err
}

func (s *Store) KnowledgeChunks(ctx context.Context, sourceID int64) ([]knowledge.Chunk, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,source_id,ordinal,heading,block_id,content,content_hash
 FROM knowledge_chunks WHERE source_id=? ORDER BY ordinal`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []knowledge.Chunk
	for rows.Next() {
		var chunk knowledge.Chunk
		if err := rows.Scan(&chunk.ID, &chunk.SourceID, &chunk.Ordinal, &chunk.Heading, &chunk.BlockID, &chunk.Content, &chunk.ContentHash); err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, rows.Err()
}

func (s *Store) ReplaceKnowledgeSource(ctx context.Context, source knowledge.Source, chunks []knowledge.Chunk) (knowledge.Source, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return source, err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	if source.SyncedAt == 0 {
		source.SyncedAt = now
	}
	if source.ID == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO knowledge_sources
 (source_type,source_url,title,scope_type,scope_id,document_id,revision_id,status,error,created_at,updated_at,synced_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(source_url) DO UPDATE SET source_type=excluded.source_type,title=excluded.title,
 scope_type=excluded.scope_type,scope_id=excluded.scope_id,document_id=excluded.document_id,
 revision_id=excluded.revision_id,status='ready',error='',updated_at=excluded.updated_at,synced_at=excluded.synced_at`,
			source.Type, source.URL, source.Title, source.ScopeType, source.ScopeID, source.DocumentID, source.Revision,
			"ready", "", now, now, source.SyncedAt)
		if err != nil {
			return source, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT id,created_at FROM knowledge_sources WHERE source_url=?`, source.URL).Scan(&source.ID, &source.CreatedAt); err != nil {
			return source, err
		}
	} else {
		result, updateErr := tx.ExecContext(ctx, `UPDATE knowledge_sources SET source_type=?,source_url=?,title=?,scope_type=?,scope_id=?,
 document_id=?,revision_id=?,status='ready',error='',updated_at=?,synced_at=? WHERE id=?`,
			source.Type, source.URL, source.Title, source.ScopeType, source.ScopeID, source.DocumentID, source.Revision,
			now, source.SyncedAt, source.ID)
		if updateErr != nil {
			return source, updateErr
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return source, fmt.Errorf("知识源不存在")
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunks_fts WHERE source_id=?`, source.ID); err != nil {
		return source, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_summaries WHERE source_id=?`, source.ID); err != nil {
		return source, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunks WHERE source_id=?`, source.ID); err != nil {
		return source, err
	}
	for _, chunk := range chunks {
		result, insertErr := tx.ExecContext(ctx, `INSERT INTO knowledge_chunks(source_id,ordinal,heading,block_id,content,content_hash)
 VALUES(?,?,?,?,?,?)`, source.ID, chunk.Ordinal, chunk.Heading, chunk.BlockID, chunk.Content, chunk.ContentHash)
		if insertErr != nil {
			return source, insertErr
		}
		chunkID, insertErr := result.LastInsertId()
		if insertErr != nil {
			return source, insertErr
		}
		if _, insertErr = tx.ExecContext(ctx, `INSERT INTO knowledge_chunks_fts(chunk_id,source_id,title,heading,content) VALUES(?,?,?,?,?)`,
			chunkID, source.ID, source.Title, chunk.Heading, chunk.Content); insertErr != nil {
			return source, insertErr
		}
	}
	if err := tx.Commit(); err != nil {
		return source, err
	}
	source.Status = "ready"
	source.Error = ""
	source.UpdatedAt = now
	source.ChunkCount = len(chunks)
	return source, nil
}

func (s *Store) MarkKnowledgeSourceError(ctx context.Context, id int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE knowledge_sources SET status='error',error=?,updated_at=? WHERE id=?`, message, time.Now().UnixMilli(), id)
	return err
}

func (s *Store) DeleteKnowledgeSource(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunks_fts WHERE source_id=?`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM knowledge_sources WHERE id=?`, id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return fmt.Errorf("知识源不存在")
	}
	return tx.Commit()
}

func (s *Store) SearchKnowledge(ctx context.Context, query string, options knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	ftsQuery := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	results, err := s.searchKnowledgeFTS(ctx, ftsQuery, options)
	if err == nil && len(results) > 0 {
		return results, nil
	}
	return s.searchKnowledgeLike(ctx, query, options)
}

func (s *Store) searchKnowledgeFTS(ctx context.Context, query string, options knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,s.id,s.title,c.heading,c.block_id,c.content,s.source_url,-bm25(knowledge_chunks_fts) AS score
 FROM knowledge_chunks_fts
 JOIN knowledge_chunks c ON c.id=knowledge_chunks_fts.chunk_id
 JOIN knowledge_sources s ON s.id=c.source_id
 WHERE knowledge_chunks_fts MATCH ? AND s.status='ready'
 AND (?='' OR s.scope_type='global' OR (s.scope_type=? AND s.scope_id=?))
 ORDER BY bm25(knowledge_chunks_fts),s.synced_at DESC LIMIT ?`, query, options.ScopeType, options.ScopeType, options.ScopeID, options.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanKnowledgeResults(rows)
}

func (s *Store) searchKnowledgeLike(ctx context.Context, query string, options knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	pattern := "%" + query + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,s.id,s.title,c.heading,c.block_id,c.content,s.source_url,0
 FROM knowledge_chunks c JOIN knowledge_sources s ON s.id=c.source_id
 WHERE s.status='ready' AND (c.content LIKE ? OR c.heading LIKE ? OR s.title LIKE ?)
 AND (?='' OR s.scope_type='global' OR (s.scope_type=? AND s.scope_id=?))
 ORDER BY s.synced_at DESC,c.ordinal LIMIT ?`, pattern, pattern, pattern, options.ScopeType, options.ScopeType, options.ScopeID, options.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanKnowledgeResults(rows)
}

type rowScanner interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanKnowledgeResults(rows rowScanner) ([]knowledge.SearchResult, error) {
	var results []knowledge.SearchResult
	for rows.Next() {
		var result knowledge.SearchResult
		if err := rows.Scan(&result.ChunkID, &result.SourceID, &result.Title, &result.Heading, &result.BlockID,
			&result.Content, &result.URL, &result.Score); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
