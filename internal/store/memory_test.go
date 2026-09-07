package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/memory"
	"github.com/youdisn/lark-ob/internal/model"
)

func TestMemoryAggregatesPersonAcrossChatsAndKeepsProvenance(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	for _, chatID := range []string{"chat-1", "chat-2"} {
		if err := store.UpsertChat(ctx, model.Chat{ID: chatID, Name: chatID, Type: "group"}); err != nil {
			t.Fatal(err)
		}
		_, err := store.ReplaceChatMemoryClaims(ctx, chatID, []memory.Claim{{
			SubjectType: memory.SubjectPerson, SubjectID: "user-a", SubjectName: "用户 A", ChatID: chatID,
			Category: "fact", Content: "来自 " + chatID, Confidence: .8, EvidenceMessageIDs: []string{"m-" + chatID},
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	claims, err := store.SearchMemoryContext(ctx, memory.ContextRequest{ChatID: "chat-1", PersonIDs: []string{"user-a"}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("got %d claims, want person memories from both chats", len(claims))
	}
	if claims[0].ChatID == claims[1].ChatID {
		t.Fatalf("chat provenance was lost: %#v", claims)
	}
}

func TestMemoryWarehouseListStatsAndDelete(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "warehouse.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err := store.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "研发群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	claims, err := store.ReplaceChatMemoryClaims(ctx, "chat-1", []memory.Claim{
		{SubjectType: memory.SubjectPerson, SubjectID: "user-a", SubjectName: "用户 A", Category: "preference", Content: "偏好简短回复", Confidence: .8, LastEvidenceAt: 123},
		{SubjectType: memory.SubjectChat, SubjectID: "chat-1", SubjectName: "研发群", Category: "project_context", Content: "正在研发", Confidence: .9, LastEvidenceAt: 456},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertChat(ctx, model.Chat{ID: "chat-2", Name: "用户 B", Type: "p2p"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceChatMemoryClaims(ctx, "chat-2", []memory.Claim{{
		SubjectType: memory.SubjectChat, SubjectID: "chat-2", SubjectName: "用户 B", Category: "relationship", Content: "单聊上下文", Confidence: .7,
	}}); err != nil {
		t.Fatal(err)
	}
	var indexed int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_claims_fts`).Scan(&indexed); err != nil || indexed != 3 {
		t.Fatalf("full-text index count = %d, want 3: %v", indexed, err)
	}
	stats, err := store.MemoryStats(ctx)
	if err != nil || stats.Total != 3 || stats.People != 1 || stats.Groups != 1 || stats.P2PChats != 1 || stats.GroupMemories != 1 || stats.P2PMemories != 1 {
		t.Fatalf("unexpected stats: %#v, %v", stats, err)
	}
	items, err := store.ListMemoryClaims(ctx, memory.ListRequest{SubjectType: memory.SubjectPerson, Query: "简短", Limit: 10})
	if err != nil || len(items) != 1 || items[0].SourceChatName != "研发群" || items[0].LastEvidenceAt != 123 {
		t.Fatalf("unexpected items: %#v, %v", items, err)
	}
	groupItems, err := store.ListMemoryClaims(ctx, memory.ListRequest{SubjectType: memory.SubjectChat, SourceChatType: "group", Limit: 10})
	if err != nil || len(groupItems) != 1 || groupItems[0].SourceChatType != "group" || groupItems[0].SubjectName != "研发群" {
		t.Fatalf("unexpected group items: %#v, %v", groupItems, err)
	}
	if err := store.DeleteMemoryClaim(ctx, claims[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_claims_fts`).Scan(&indexed); err != nil || indexed != 2 {
		t.Fatalf("full-text index count after delete = %d, want 2: %v", indexed, err)
	}
	if err := store.DeleteMemoryClaim(ctx, claims[0].ID); err != sql.ErrNoRows {
		t.Fatalf("second delete error = %v, want sql.ErrNoRows", err)
	}
}

func TestManualMemorySurvivesAgentReplacementAndHonorsValidity(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "manual-memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	manual, err := st.SaveManualMemoryClaim(ctx, memory.Claim{SubjectType: memory.SubjectPerson, SubjectID: "u1", SubjectName: "用户一",
		ChatID: "chat-1", Category: "constraint", Content: "目前异地，不建议线下见面", Importance: memory.ImportanceConstraint,
		ValidFrom: now - 1000, ValidUntil: now + 60_000, Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceChatMemoryClaims(ctx, "chat-1", []memory.Claim{{SubjectType: memory.SubjectChat, SubjectID: "chat-1", Content: "自动记忆", Category: "fact", Confidence: .8}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceChatMemoryClaims(ctx, "chat-1", []memory.Claim{{SubjectType: memory.SubjectChat, SubjectID: "chat-1", Content: "更新后的自动记忆", Category: "fact", Confidence: .8}}); err != nil {
		t.Fatal(err)
	}
	items, err := st.ListMemoryClaims(ctx, memory.ListRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d memories, want one manual and one agent memory", len(items))
	}
	var found bool
	for _, item := range items {
		if item.ID == manual.ID {
			found = item.SourceType == memory.SourceManual && item.Pinned
		}
	}
	if !found {
		t.Fatalf("manual memory was overwritten: %#v", items)
	}
	active, err := st.SearchMemoryContext(ctx, memory.ContextRequest{ChatID: "chat-1", PersonIDs: []string{"u1"}, ActiveAt: now, Limit: 10})
	if err != nil || len(active) != 2 {
		t.Fatalf("active context = %#v, %v", active, err)
	}
	expired, err := st.SearchMemoryContext(ctx, memory.ContextRequest{ChatID: "chat-1", PersonIDs: []string{"u1"}, ActiveAt: now + 120_000, Limit: 10})
	if err != nil || len(expired) != 1 || expired[0].SourceType != memory.SourceAgent {
		t.Fatalf("expired context = %#v, %v", expired, err)
	}
}

func TestKnowledgeSummaryRoundTrip(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "summary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	source, err := store.ReplaceKnowledgeSource(ctx, knowledge.Source{Type: "lark_doc", URL: "https://example.test/doc", Title: "文档", ScopeType: knowledge.ScopeGlobal}, []knowledge.Chunk{{Ordinal: 0, Content: "内容", ContentHash: "h"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.SaveKnowledgeSummary(ctx, knowledge.Summary{SourceID: source.ID, Summary: "摘要", Facts: []string{"事实"}, Model: "grok-4.5"})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Summary != "摘要" || len(saved.Facts) != 1 || saved.Model != "grok-4.5" {
		t.Fatalf("unexpected summary: %#v", saved)
	}
	if _, err := store.ReplaceKnowledgeSource(ctx, knowledge.Source{ID: source.ID, Type: "lark_doc", URL: source.URL, Title: "更新文档", ScopeType: knowledge.ScopeGlobal}, []knowledge.Chunk{{Ordinal: 0, Content: "更新内容", ContentHash: "h2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetKnowledgeSummary(ctx, source.ID); err == nil {
		t.Fatal("summary must be invalidated when source content is replaced")
	}
}
