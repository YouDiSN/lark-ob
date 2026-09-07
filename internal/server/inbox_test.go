package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/memory"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/store"
)

func TestMarkChatViewedAPIUsesLocalWatermark(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "inbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMessage(ctx, model.Message{ID: "mention", ChatID: "chat-1", SenderID: "u1", Content: "@我 请确认", CreatedAt: 100, MentionsSelf: true}); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: st, clients: map[chan struct{}]struct{}{}}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/chats/chat-1/viewed", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("viewed status = %d, body = %s", response.Code, response.Body.String())
	}
	chat, err := st.Chat(ctx, "chat-1")
	if err != nil || chat.NewMessages != 0 || chat.MentionCount != 0 {
		t.Fatalf("activity was not cleared: %#v, %v", chat, err)
	}
}

func TestInboxBulkReadAndGroupPreferencesAPI(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "inbox-preferences.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertChat(ctx, model.Chat{ID: "group-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMessage(ctx, model.Message{ID: "new", ChatID: "group-1", SenderID: "u1", Content: "新消息", CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	memories := memory.NewService(st)
	if _, err := memories.ReplaceChat(ctx, "group-1", []memory.Claim{{
		SubjectType: memory.SubjectChat, SubjectID: "group-1", Content: "旧记忆", Confidence: 0.8,
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SavePersonImpression(ctx, impression.PersonImpression{PersonID: "u1", PersonName: "用户", Summary: "旧印象"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveStyleProfile(ctx, impression.StyleProfile{ScopeType: impression.ScopeSelf, ScopeID: "me", Summary: "旧风格"}); err != nil {
		t.Fatal(err)
	}
	server := &Server{store: st, memories: memories, clients: map[chan struct{}]struct{}{}}

	body := bytes.NewBufferString(`{"muted":true,"inMessageBox":true}`)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPatch, "/api/chats/group-1/preferences", body))
	if response.Code != http.StatusOK {
		t.Fatalf("preferences status = %d, body = %s", response.Code, response.Body.String())
	}
	var updated model.Chat
	if err := json.Unmarshal(response.Body.Bytes(), &updated); err != nil || !updated.Muted || !updated.InMessageBox {
		t.Fatalf("unexpected updated chat: %#v, %v", updated, err)
	}
	claims, err := memories.ListChat(ctx, "group-1")
	if err != nil || len(claims) != 0 {
		t.Fatalf("message-box memory was not cleared: %#v, %v", claims, err)
	}
	if _, err := st.GetPersonImpression(ctx, "u1"); err != sql.ErrNoRows {
		t.Fatalf("message-box person impression was not invalidated: %v", err)
	}
	if _, err := st.GetStyleProfile(ctx, impression.ScopeSelf, "me"); err != sql.ErrNoRows {
		t.Fatalf("self style was not invalidated: %v", err)
	}

	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/chats/read-all", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("read-all status = %d, body = %s", response.Code, response.Body.String())
	}
	chat, err := st.Chat(ctx, "group-1")
	if err != nil || chat.NewMessages != 0 {
		t.Fatalf("activity was not cleared: %#v, %v", chat, err)
	}
}
