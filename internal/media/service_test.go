package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/store"
)

type fakeDownloader struct{ calls int }

func (f *fakeDownloader) DownloadImage(_ context.Context, _, _, dir, filename string) error {
	f.calls++
	return os.WriteFile(filepath.Join(dir, filename), []byte("\x89PNG\r\n\x1a\nimage"), 0o600)
}

func TestImageDownloadsOnceAndRequiresMessageResourceMatch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "media.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.UpsertChat(ctx, model.Chat{ID: "chat-1", Name: "群聊", Type: "group"}); err != nil {
		t.Fatal(err)
	}
	messageID, imageKey := "om_message1", "img_image1"
	if err := st.UpsertMessage(ctx, model.Message{ID: messageID, ChatID: "chat-1", Type: "image", Content: "[Image: " + imageKey + "]", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	downloader := &fakeDownloader{}
	service := New(st, downloader, filepath.Join(t.TempDir(), "cache"))
	for range 2 {
		path, contentType, err := service.Image(ctx, messageID, imageKey)
		if err != nil || contentType != "image/png" {
			t.Fatalf("image result = %q, %q, %v", path, contentType, err)
		}
	}
	if downloader.calls != 1 {
		t.Fatalf("download calls = %d, want 1", downloader.calls)
	}
	if _, _, err := service.Image(ctx, messageID, "img_other"); !os.IsNotExist(err) {
		t.Fatalf("unrelated resource should be hidden, got %v", err)
	}
}
