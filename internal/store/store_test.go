package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/youdisn/lark-ob/internal/model"
)

func TestUpsertAndRead(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "真实会话", Type: "p2p"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "m1", ChatID: "chat-1", SenderID: "u1", SenderName: "成员", Content: "第一条", Type: "text", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "m2", ChatID: "chat-1", SenderID: "self", SenderName: "我", Content: "第二条", Type: "text", CreatedAt: 2, IsSelf: true}); err != nil {
		t.Fatal(err)
	}
	chats, err := s.Chats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 {
		t.Fatalf("got %d chats, want 1", len(chats))
	}
	msgs, err := s.Messages(ctx, "chat-1", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].CreatedAt >= msgs[len(msgs)-1].CreatedAt {
		t.Fatal("messages are not ordered oldest first")
	}
}
