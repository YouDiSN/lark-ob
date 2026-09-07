package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/youdisn/lark-ob/internal/memory"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/store"
)

func TestMemoryWarehouseAPIContract(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "研发群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	claims, err := st.ReplaceChatMemoryClaims(ctx, "chat-1", []memory.Claim{
		{SubjectType: memory.SubjectPerson, SubjectID: "user-a", SubjectName: "用户 A", Category: "fact", Content: "负责服务端", Confidence: .8, LastEvidenceAt: time.Now().UnixMilli()},
		{SubjectType: memory.SubjectChat, SubjectID: "chat-1", SubjectName: "研发群", Category: "project_context", Content: "群组上下文", Confidence: .7, LastEvidenceAt: time.Now().UnixMilli()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-2", Name: "用户 B", Type: "p2p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceChatMemoryClaims(ctx, "chat-2", []memory.Claim{{
		SubjectType: memory.SubjectChat, SubjectID: "chat-2", SubjectName: "用户 B", Category: "relationship", Content: "单聊上下文", Confidence: .6,
	}}); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: st, memories: memory.NewService(st), clients: map[chan struct{}]struct{}{}}
	handler := server.Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/memories?subjectType=person&q=服务端", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	var warehouse memory.Warehouse
	if err := json.Unmarshal(response.Body.Bytes(), &warehouse); err != nil {
		t.Fatal(err)
	}
	if warehouse.Stats.Total != 3 || len(warehouse.Items) != 1 || warehouse.Items[0].EffectiveScore <= 0 {
		t.Fatalf("unexpected warehouse: %#v", warehouse)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/memories?subjectType=chat&sourceChatType=group", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("group list status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &warehouse); err != nil {
		t.Fatal(err)
	}
	if len(warehouse.Items) != 1 || warehouse.Items[0].SourceChatType != "group" || warehouse.Items[0].SubjectName != "研发群" {
		t.Fatalf("group filter leaked non-group memories: %#v", warehouse.Items)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/memories?limit=1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("paged list status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &warehouse); err != nil {
		t.Fatal(err)
	}
	if len(warehouse.Items) != 1 || warehouse.TotalMatches != 3 || !warehouse.HasMore || warehouse.NextOffset != 1 {
		t.Fatalf("unexpected pagination: %#v", warehouse)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/memories/%d", claims[0].ID), nil))
	if response.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", response.Code, response.Body.String())
	}
}
