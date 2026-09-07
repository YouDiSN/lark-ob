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
	if chats[0].LastMessage != "第二条" || chats[0].LastTime != 2 {
		t.Fatalf("chat preview is not the newest message: %#v", chats[0])
	}
	msgs, err := s.Messages(ctx, "chat-1", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if msgs[0].CreatedAt >= msgs[len(msgs)-1].CreatedAt {
		t.Fatal("messages are not ordered chronologically")
	}
}

func TestMessagesUsePositionForSameMinuteOrderingAndPreview(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "position.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "会话", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	// The CLI returns newest first. An older message inserted afterwards must
	// not replace the newest preview merely because both display as 08:00.
	if err := s.UpsertMessage(ctx, model.Message{ID: "newer", ChatID: "chat-1", Content: "最新", CreatedAt: 100, Position: 12}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "older", ChatID: "chat-1", Content: "较早", CreatedAt: 100, Position: 11}); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, "chat-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].ID != "older" || messages[1].ID != "newer" {
		t.Fatalf("same-minute messages are out of order: %#v", messages)
	}
	chat, err := s.Chat(ctx, "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if chat.LastMessage != "最新" || chat.LastPosition != 12 {
		t.Fatalf("chat preview does not use position: %#v", chat)
	}
}

func TestUpsertMessageRepairsTimestampAndLatestPreview(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "repair.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "会话", Type: "p2p"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "m1", ChatID: "chat-1", Content: "旧解析", CreatedAt: 0}); err != nil {
		t.Fatal(err)
	}
	if !s.HasMessagesWithoutTimestamp(ctx) {
		t.Fatal("zero timestamp should request a repair sync")
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "m1", ChatID: "chat-1", Content: "已修复", CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	if s.HasMessagesWithoutTimestamp(ctx) {
		t.Fatal("repaired timestamps should not request another repair sync")
	}
	messages, err := s.Messages(ctx, "chat-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].CreatedAt != 100 || messages[0].Content != "已修复" {
		t.Fatalf("message was not repaired: %#v", messages)
	}
	chats, err := s.Chats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if chats[0].LastTime != 100 || chats[0].LastMessage != "已修复" {
		t.Fatalf("chat preview was not repaired: %#v", chats[0])
	}
}

func TestRepairMessageTimestampsBackfillsHistoricalRows(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "batch-repair.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "会话", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "old", ChatID: "chat-1", Content: "历史消息", CreatedAt: 0}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.MessageIDsWithoutTimestamp(ctx)
	if err != nil || len(ids) != 1 || ids[0] != "old" {
		t.Fatalf("unexpected missing timestamp IDs: %v, %v", ids, err)
	}
	repaired, err := s.RepairMessageTimestamps(ctx, []model.Message{{ID: "old", CreatedAt: 1234}})
	if err != nil || repaired != 1 {
		t.Fatalf("repair result = %d, %v", repaired, err)
	}
	messages, err := s.Messages(ctx, "chat-1", 10)
	if err != nil || len(messages) != 1 || messages[0].CreatedAt != 1234 {
		t.Fatalf("timestamp was not repaired: %#v, %v", messages, err)
	}
	chats, err := s.Chats(ctx)
	if err != nil || len(chats) != 1 || chats[0].LastTime != 1234 {
		t.Fatalf("chat preview was not rebuilt: %#v, %v", chats, err)
	}
}

func TestMessagesSinceAppliesTimeWindow(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "window.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "会话", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []model.Message{
		{ID: "old", ChatID: "chat-1", Content: "窗口外", CreatedAt: 100},
		{ID: "new", ChatID: "chat-1", Content: "窗口内", CreatedAt: 200},
	} {
		if err := s.UpsertMessage(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	messages, err := s.MessagesSince(ctx, "chat-1", 150, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].ID != "new" {
		t.Fatalf("unexpected window messages: %#v", messages)
	}
}

func TestChatActivityTracksLocalNewMessagesAndMentions(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "baseline", ChatID: "chat-1", SenderID: "u1", Content: "历史消息", CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkChatViewed(ctx, "chat-1"); err != nil {
		t.Fatal(err)
	}
	for _, message := range []model.Message{
		{ID: "normal", ChatID: "chat-1", SenderID: "u2", Content: "普通新消息", CreatedAt: 200},
		{ID: "mention", ChatID: "chat-1", SenderID: "u3", Content: "@我 请确认", CreatedAt: 300, Mentions: []model.Mention{{ID: "self", Name: "我"}}, MentionsSelf: true},
		{ID: "self", ChatID: "chat-1", SenderID: "self", Content: "自己的消息", CreatedAt: 400, IsSelf: true},
	} {
		if err := s.UpsertMessage(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	chat, err := s.Chat(ctx, "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if chat.NewMessages != 2 || chat.MentionCount != 1 {
		t.Fatalf("unexpected local activity: %#v", chat)
	}
	messages, err := s.Messages(ctx, "chat-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 || !messages[2].MentionsSelf || len(messages[2].Mentions) != 1 {
		t.Fatalf("mention metadata did not round trip: %#v", messages)
	}
	if err := s.MarkChatViewed(ctx, "chat-1"); err != nil {
		t.Fatal(err)
	}
	chat, err = s.Chat(ctx, "chat-1")
	if err != nil || chat.NewMessages != 0 || chat.MentionCount != 0 {
		t.Fatalf("view watermark did not clear activity: %#v, %v", chat, err)
	}
}

func TestMarkAllChatsViewedAndChatPreferencesPersistAcrossSync(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "preferences.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, chat := range []model.Chat{{ID: "group-1", Name: "项目群", Type: "group"}, {ID: "group-2", Name: "闲聊群", Type: "group"}} {
		if err := s.UpsertChat(ctx, chat); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertMessage(ctx, model.Message{ID: "message-" + chat.ID, ChatID: chat.ID, SenderID: "other", Content: "新消息", CreatedAt: 100, MentionsSelf: true}); err != nil {
			t.Fatal(err)
		}
	}
	muted, boxed := true, true
	if err := s.SetChatPreferences(ctx, "group-1", &muted, &boxed); err != nil {
		t.Fatal(err)
	}
	// A later Lark sync updates server-owned metadata but must not overwrite the
	// user's local inbox preferences.
	if err := s.UpsertChat(ctx, model.Chat{ID: "group-1", Name: "项目群（新名称）", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	chat, err := s.Chat(ctx, "group-1")
	if err != nil || !chat.Muted || !chat.InMessageBox {
		t.Fatalf("preferences did not persist: %#v, %v", chat, err)
	}
	updated, err := s.MarkAllChatsViewed(ctx)
	if err != nil || updated != 2 {
		t.Fatalf("mark all result = %d, %v", updated, err)
	}
	chats, err := s.Chats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range chats {
		if item.NewMessages != 0 || item.MentionCount != 0 {
			t.Fatalf("chat activity was not cleared: %#v", item)
		}
	}
}

func TestChatPreferencesRejectDirectMessages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "p2p-preferences.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "p2p-1", Name: "用户", Type: "p2p"}); err != nil {
		t.Fatal(err)
	}
	muted := true
	if err := s.SetChatPreferences(ctx, "p2p-1", &muted, nil); err == nil {
		t.Fatal("direct message preference should be rejected")
	}
}

func TestReclassifyMessagesRepairsExistingIdentityFlags(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	for _, message := range []model.Message{
		{ID: "self", ChatID: "chat-1", SenderID: "ou_self", Content: "我的消息", CreatedAt: 100},
		{ID: "mention", ChatID: "chat-1", SenderID: "ou_other", Content: "@我 请确认", CreatedAt: 200, Mentions: []model.Mention{{ID: "ou_self", Name: "我"}}},
	} {
		if err := s.UpsertMessage(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReclassifyMessages(ctx, "ou_self"); err != nil {
		t.Fatal(err)
	}
	messages, err := s.Messages(ctx, "chat-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || !messages[0].IsSelf || messages[0].MentionsSelf || messages[1].IsSelf || !messages[1].MentionsSelf {
		t.Fatalf("messages were not reclassified: %#v", messages)
	}
}

func TestPersonProfileDecoratesMessagesAndP2PChat(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "profiles.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "用户", Type: "p2p"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertMessage(ctx, model.Message{ID: "message-1", ChatID: "chat-1", SenderID: "ou_user", SenderName: "用户", Content: "你好", CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPersonProfile(ctx, model.PersonProfile{ID: "ou_user", Name: "用户", Avatar: "https://example/avatar.png", Base: "重庆", Department: "技术-研发", CheckedAt: 200}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertPersonProfile(ctx, model.PersonProfile{ID: "ou_user", Name: "用户", CheckedAt: 300}); err != nil {
		t.Fatal(err)
	}
	profile, err := s.PersonProfile(ctx, "ou_user")
	if err != nil || profile.Base != "重庆" || profile.Department != "技术-研发" || profile.Avatar != "https://example/avatar.png" {
		t.Fatalf("partial refresh erased profile fields: %#v, %v", profile, err)
	}
	messages, err := s.Messages(ctx, "chat-1", 10)
	if err != nil || len(messages) != 1 || messages[0].SenderAvatar != "https://example/avatar.png" {
		t.Fatalf("message avatar missing: %#v, %v", messages, err)
	}
	chat, err := s.Chat(ctx, "chat-1")
	if err != nil || chat.Avatar != "https://example/avatar.png" {
		t.Fatalf("p2p chat avatar missing: %#v, %v", chat, err)
	}
}
