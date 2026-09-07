package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/youdisn/lark-ob/internal/knowledge"
)

func TestKnowledgeSourceReplaceAndSearch(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "knowledge.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	source, err := store.ReplaceKnowledgeSource(ctx, knowledge.Source{
		Type: "lark_doc", URL: "https://example.feishu.cn/docx/a", Title: "发布手册",
		ScopeType: knowledge.ScopeGlobal, DocumentID: "doc-a", Revision: 1,
	}, []knowledge.Chunk{{Ordinal: 0, Heading: "生产发布", BlockID: "h1", Content: "创建部署标签", ContentHash: "hash-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if source.ID == 0 || source.ChunkCount != 1 {
		t.Fatalf("unexpected source: %#v", source)
	}
	results, err := store.SearchKnowledge(ctx, "部署", knowledge.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].SourceID != source.ID || results[0].BlockID != "h1" {
		t.Fatalf("unexpected search results: %#v", results)
	}
	_, err = store.ReplaceKnowledgeSource(ctx, knowledge.Source{
		ID: source.ID, Type: "lark_doc", URL: source.URL, Title: "新发布手册", ScopeType: knowledge.ScopeGlobal,
		DocumentID: "doc-a", Revision: 2,
	}, []knowledge.Chunk{{Ordinal: 0, Heading: "回滚", Content: "发生异常时回滚", ContentHash: "hash-2"}})
	if err != nil {
		t.Fatal(err)
	}
	oldResults, err := store.SearchKnowledge(ctx, "创建部署标签", knowledge.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldResults) != 0 {
		t.Fatalf("old chunks must be removed: %#v", oldResults)
	}
}
