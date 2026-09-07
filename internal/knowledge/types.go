package knowledge

import "time"

const (
	ScopeGlobal  = "global"
	ScopePerson  = "person"
	ScopeChat    = "chat"
	ScopeProject = "project"
)

type Source struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	URL        string `json:"url"`
	Title      string `json:"title"`
	ScopeType  string `json:"scopeType"`
	ScopeID    string `json:"scopeId,omitempty"`
	DocumentID string `json:"documentId"`
	Revision   int64  `json:"revision"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	ChunkCount int    `json:"chunkCount"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	SyncedAt   int64  `json:"syncedAt"`
}

type Document struct {
	SourceType string
	URL        string
	Title      string
	DocumentID string
	Revision   int64
	Content    string
	FetchedAt  time.Time
}

type Chunk struct {
	ID          int64  `json:"id"`
	SourceID    int64  `json:"sourceId"`
	Ordinal     int    `json:"ordinal"`
	Heading     string `json:"heading,omitempty"`
	BlockID     string `json:"blockId,omitempty"`
	Content     string `json:"content"`
	ContentHash string `json:"-"`
}

type SearchResult struct {
	ChunkID  int64   `json:"chunkId"`
	SourceID int64   `json:"sourceId"`
	Title    string  `json:"title"`
	Heading  string  `json:"heading,omitempty"`
	BlockID  string  `json:"blockId,omitempty"`
	Content  string  `json:"content"`
	URL      string  `json:"url"`
	Score    float64 `json:"score"`
}

type ImportRequest struct {
	URL       string `json:"url"`
	ScopeType string `json:"scopeType"`
	ScopeID   string `json:"scopeId,omitempty"`
}

type SearchOptions struct {
	ScopeType string
	ScopeID   string
	Limit     int
}

type Summary struct {
	SourceID  int64    `json:"sourceId"`
	Summary   string   `json:"summary"`
	Facts     []string `json:"facts"`
	Model     string   `json:"model"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}
