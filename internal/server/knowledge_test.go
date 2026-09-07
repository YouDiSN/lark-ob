package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/store"
)

type knowledgeReaderStub struct{}

func (knowledgeReaderStub) Fetch(_ context.Context, sourceURL string) (knowledge.Document, error) {
	return knowledge.Document{
		SourceType: "lark_doc",
		URL:        sourceURL,
		Title:      "发布手册",
		DocumentID: "doc-1",
		Revision:   3,
		Content:    `<doc><heading id="block-1">发布</heading><p>创建部署标签</p></doc>`,
		FetchedAt:  time.Unix(10, 0),
	}, nil
}

func TestKnowledgeAPIContract(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	server := &Server{
		store:     st,
		knowledge: knowledge.NewService(st, knowledgeReaderStub{}),
		clients:   map[chan struct{}]struct{}{},
	}
	handler := server.Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/knowledge/sources", strings.NewReader(`{
  "url":"https://example.feishu.cn/docx/doc-1",
  "scopeType":"global"
}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("import status = %d, body = %s", response.Code, response.Body.String())
	}
	var source knowledge.Source
	if err := json.Unmarshal(response.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if source.URL != "https://example.feishu.cn/docx/doc-1" || source.Title != "发布手册" || source.ChunkCount == 0 {
		t.Fatalf("unexpected source response: %#v", source)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/knowledge/search?q=部署", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", response.Code, response.Body.String())
	}
	var results []knowledge.SearchResult
	if err := json.Unmarshal(response.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Title != "发布手册" || results[0].URL == "" {
		t.Fatalf("unexpected search response: %#v", results)
	}
}
