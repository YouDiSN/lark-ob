package knowledge

import (
	"context"
	"fmt"
	"strings"
)

type Reader interface {
	Fetch(context.Context, string) (Document, error)
}

type Repository interface {
	ListKnowledgeSources(context.Context) ([]Source, error)
	GetKnowledgeSource(context.Context, int64) (Source, error)
	ReplaceKnowledgeSource(context.Context, Source, []Chunk) (Source, error)
	MarkKnowledgeSourceError(context.Context, int64, string) error
	DeleteKnowledgeSource(context.Context, int64) error
	SearchKnowledge(context.Context, string, SearchOptions) ([]SearchResult, error)
}

type Service struct {
	repository Repository
	reader     Reader
}

func NewService(repository Repository, reader Reader) *Service {
	return &Service{repository: repository, reader: reader}
}

func (s *Service) Import(ctx context.Context, request ImportRequest) (Source, error) {
	request.URL = strings.TrimSpace(request.URL)
	request.ScopeType = strings.TrimSpace(request.ScopeType)
	request.ScopeID = strings.TrimSpace(request.ScopeID)
	if request.ScopeType == "" {
		request.ScopeType = ScopeGlobal
	}
	if request.URL == "" {
		return Source{}, fmt.Errorf("请输入飞书文档或 Wiki 链接")
	}
	if err := validateScope(request.ScopeType, request.ScopeID); err != nil {
		return Source{}, err
	}
	document, err := s.reader.Fetch(ctx, request.URL)
	if err != nil {
		return Source{}, err
	}
	chunks := ChunkDocument(document.Content, 1400)
	if len(chunks) == 0 {
		return Source{}, fmt.Errorf("文档没有可索引的正文")
	}
	source := Source{
		Type:       document.SourceType,
		URL:        request.URL,
		Title:      document.Title,
		ScopeType:  request.ScopeType,
		ScopeID:    request.ScopeID,
		DocumentID: document.DocumentID,
		Revision:   document.Revision,
		Status:     "ready",
		SyncedAt:   document.FetchedAt.UnixMilli(),
	}
	return s.repository.ReplaceKnowledgeSource(ctx, source, chunks)
}

func (s *Service) Sync(ctx context.Context, id int64) (Source, error) {
	source, err := s.repository.GetKnowledgeSource(ctx, id)
	if err != nil {
		return Source{}, err
	}
	document, err := s.reader.Fetch(ctx, source.URL)
	if err != nil {
		_ = s.repository.MarkKnowledgeSourceError(ctx, id, err.Error())
		return Source{}, err
	}
	chunks := ChunkDocument(document.Content, 1400)
	if len(chunks) == 0 {
		err := fmt.Errorf("文档没有可索引的正文")
		_ = s.repository.MarkKnowledgeSourceError(ctx, id, err.Error())
		return Source{}, err
	}
	source.Type = document.SourceType
	source.Title = document.Title
	source.DocumentID = document.DocumentID
	source.Revision = document.Revision
	source.Status = "ready"
	source.Error = ""
	source.SyncedAt = document.FetchedAt.UnixMilli()
	return s.repository.ReplaceKnowledgeSource(ctx, source, chunks)
}

func (s *Service) List(ctx context.Context) ([]Source, error) {
	return s.repository.ListKnowledgeSources(ctx)
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.repository.DeleteKnowledgeSource(ctx, id)
}

func (s *Service) Search(ctx context.Context, query string, options SearchOptions) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchResult{}, nil
	}
	if options.ScopeType != "" {
		if err := validateScope(options.ScopeType, options.ScopeID); err != nil {
			return nil, err
		}
	}
	if options.Limit <= 0 || options.Limit > 50 {
		options.Limit = 10
	}
	return s.repository.SearchKnowledge(ctx, query, options)
}

func validateScope(scopeType, scopeID string) error {
	if scopeType == "" {
		scopeType = ScopeGlobal
	}
	switch scopeType {
	case ScopeGlobal:
		if scopeID != "" {
			return fmt.Errorf("全局知识不需要 scopeId")
		}
	case ScopePerson, ScopeChat, ScopeProject:
		if scopeID == "" {
			return fmt.Errorf("%s 知识必须提供 scopeId", scopeType)
		}
	default:
		return fmt.Errorf("不支持的知识范围 %q", scopeType)
	}
	return nil
}
