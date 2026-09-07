package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/model"
)

func TestImpressionStoreAggregatesPeopleAndVersionsSnapshots(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "impressions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-box", Name: "消息盒子群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	inMessageBox := true
	if err := st.SetChatPreferences(ctx, "chat-box", nil, &inMessageBox); err != nil {
		t.Fatal(err)
	}
	for _, message := range []model.Message{
		{ID: "m1", ChatID: "chat-1", SenderID: "person-a", SenderName: "小 A", Content: "我们明天确认", CreatedAt: now - 1000},
		{ID: "m2", ChatID: "chat-1", SenderID: "self", SenderName: "我", Content: "可以，我整理一下", CreatedAt: now, IsSelf: true},
		{ID: "box-1", ChatID: "chat-box", SenderID: "person-box", SenderName: "不关心的人", Content: "营销消息", CreatedAt: now},
		{ID: "box-2", ChatID: "chat-box", SenderID: "self", SenderName: "我", Content: "自动回复", CreatedAt: now, IsSelf: true},
	} {
		if err := st.UpsertMessage(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	people, err := st.PeopleForImpression(ctx, now-10_000, 0)
	if err != nil || len(people) != 1 || people[0].ID != "person-a" {
		t.Fatalf("people = %#v, err = %v", people, err)
	}
	interaction, err := st.InteractionMessagesSince(ctx, "person-box", now-10_000, 100)
	if err != nil || len(interaction) != 0 {
		t.Fatalf("message-box interaction = %#v, err = %v", interaction, err)
	}
	selfMessages, err := st.SelfMessagesSince(ctx, now-10_000, 100)
	if err != nil || len(selfMessages) != 1 || selfMessages[0].ID != "m2" {
		t.Fatalf("self messages = %#v, err = %v", selfMessages, err)
	}
	selfCount, _, err := st.SelfMessageStats(ctx, now-10_000, 0)
	if err != nil || selfCount != 1 {
		t.Fatalf("self count = %d, err = %v", selfCount, err)
	}
	value := impression.PersonImpression{PersonID: "person-a", PersonName: "小 A", Tags: []string{"简洁"}, Summary: "偏好直接确认",
		CommunicationGuidance: "先给结论", EvidenceMessageIDs: []string{"m1"}, GeneratedAt: now}
	first, err := st.SavePersonImpression(ctx, value)
	if err != nil || first.Version != 1 {
		t.Fatalf("first = %#v, err = %v", first, err)
	}
	second, err := st.SavePersonImpression(ctx, value)
	if err != nil || second.Version != 2 {
		t.Fatalf("second = %#v, err = %v", second, err)
	}
	loaded, err := st.GetPersonImpression(ctx, "person-a")
	if err != nil || loaded.Version != 2 || len(loaded.Tags) != 1 {
		t.Fatalf("loaded = %#v, err = %v", loaded, err)
	}
}
