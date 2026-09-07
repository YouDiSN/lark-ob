package knowledge

import (
	"context"
	"testing"
	"time"
)

type fakeReader struct{ document Document }

func (f fakeReader) Fetch(context.Context, string) (Document, error) { return f.document, nil }

type fakeRepository struct {
	source Source
	chunks []Chunk
}

func (r *fakeRepository) ListKnowledgeSources(context.Context) ([]Source, error) {
	return []Source{r.source}, nil
}
func (r *fakeRepository) GetKnowledgeSource(context.Context, int64) (Source, error) {
	return r.source, nil
}
func (r *fakeRepository) ReplaceKnowledgeSource(_ context.Context, source Source, chunks []Chunk) (Source, error) {
	r.source, r.chunks = source, chunks
	return source, nil
}
func (r *fakeRepository) MarkKnowledgeSourceError(context.Context, int64, string) error { return nil }
func (r *fakeRepository) DeleteKnowledgeSource(context.Context, int64) error            { return nil }
func (r *fakeRepository) SearchKnowledge(context.Context, string, SearchOptions) ([]SearchResult, error) {
	return nil, nil
}

func TestImportDefaultsToGlobalScope(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, fakeReader{document: Document{
		SourceType: "lark_doc", URL: "https://example/docx/x", Title: "手册", DocumentID: "doc-1",
		Revision: 2, Content: "<title>手册</title><p>发布流程</p>", FetchedAt: time.Unix(10, 0),
	}})
	result, err := service.Import(context.Background(), ImportRequest{URL: "https://example/docx/x"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ScopeType != ScopeGlobal {
		t.Fatalf("scope = %q", result.ScopeType)
	}
	if len(repository.chunks) == 0 {
		t.Fatal("expected indexed chunks")
	}
}
