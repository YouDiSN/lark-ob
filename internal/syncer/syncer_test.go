package syncer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/store"
)

func TestStoreMessagesClassifiesSelfMentions(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "项目群", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	syncer := &Syncer{store: st}
	messages := []model.Message{{
		ID: "m1", ChatID: "chat-1", SenderID: "u1", Content: "请确认", CreatedAt: 100,
		Mentions: []model.Mention{{ID: "self", Name: "我"}},
	}}
	if _, err := syncer.storeMessages(ctx, messages, "self"); err != nil {
		t.Fatal(err)
	}
	stored, err := st.Messages(ctx, "chat-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || !stored[0].MentionsSelf {
		t.Fatalf("self mention was not classified: %#v", stored)
	}
}
