package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/model"
)

type fakeRepository struct {
	settings map[string][]byte
	chats    []model.Chat
}

func (f *fakeRepository) Chats(context.Context) ([]model.Chat, error) { return f.chats, nil }
func (f *fakeRepository) GetJSON(_ context.Context, key string, value any) error {
	raw, ok := f.settings[key]
	if !ok {
		return fmt.Errorf("missing")
	}
	return json.Unmarshal(raw, value)
}
func (f *fakeRepository) SetJSON(_ context.Context, key string, value any) error {
	raw, _ := json.Marshal(value)
	f.settings[key] = raw
	return nil
}

type fakeSyncer struct{ calls int }

func (f *fakeSyncer) Sync(context.Context) error { f.calls++; return nil }

type fakeExtractor struct {
	chats   []string
	cleared []string
}

func (f *fakeExtractor) ExtractChatMemorySince(_ context.Context, chatID string, _ time.Time) (agent.ChatMemoryResult, error) {
	f.chats = append(f.chats, chatID)
	return agent.ChatMemoryResult{ChatID: chatID}, nil
}
func (f *fakeExtractor) ClearChatMemory(_ context.Context, chatID string) (agent.ChatMemoryResult, error) {
	f.cleared = append(f.cleared, chatID)
	return agent.ChatMemoryResult{ChatID: chatID}, nil
}

type fakeUpdater struct {
	changedSince time.Time
	calls        int
}

func (f *fakeUpdater) Update(_ context.Context, _, changedSince time.Time) (impression.UpdateResult, error) {
	f.calls++
	f.changedSince = changedSince
	return impression.UpdateResult{ImpressionsSaved: 2, StylesSaved: 1}, nil
}

func TestRunNowRefreshesOnlyChatsChangedAfterCheckpoint(t *testing.T) {
	now := time.Now()
	repo := &fakeRepository{settings: map[string][]byte{}, chats: []model.Chat{
		{ID: "new", Name: "新会话", LastTime: now.UnixMilli()},
		{ID: "old", Name: "旧会话", LastTime: now.Add(-48 * time.Hour).UnixMilli()},
		{ID: "box", Name: "消息盒子群", LastTime: now.UnixMilli(), InMessageBox: true},
	}}
	checkpointAt := now.Add(-24 * time.Hour).UnixMilli()
	if err := repo.SetJSON(context.Background(), checkpointKey, checkpoint{LastSucceededAt: checkpointAt}); err != nil {
		t.Fatal(err)
	}
	syncer := &fakeSyncer{}
	extractor := &fakeExtractor{}
	updater := &fakeUpdater{}
	service := New(Config{LookbackDays: 30, Workers: 1, Hour: 3, Location: time.UTC}, repo, syncer, extractor, updater)
	if err := service.RunNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if syncer.calls != 1 || len(extractor.chats) != 1 || extractor.chats[0] != "new" {
		t.Fatalf("sync calls=%d extracted=%v", syncer.calls, extractor.chats)
	}
	if len(extractor.cleared) != 1 || extractor.cleared[0] != "box" {
		t.Fatalf("cleared=%v", extractor.cleared)
	}
	if updater.calls != 1 || updater.changedSince.UnixMilli() != checkpointAt {
		t.Fatalf("updater=%#v", updater)
	}
	status := service.Status()
	if status.ImpressionsSaved != 2 || status.StylesSaved != 1 || status.LastSucceededAt == 0 {
		t.Fatalf("status=%#v", status)
	}
}

func TestNextRunUsesConfiguredWallClock(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	service := New(Config{Hour: 3, Minute: 15, Location: location}, nil, nil, nil, nil)
	now := time.Date(2026, 9, 4, 4, 0, 0, 0, location)
	next := service.nextRun(now)
	if next.Day() != 5 || next.Hour() != 3 || next.Minute() != 15 {
		t.Fatalf("next=%s", next)
	}
}

func TestLastScheduledRunAllowsStartupCatchUp(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	service := New(Config{Hour: 3, Minute: 0, Location: location}, nil, nil, nil, nil)
	afterSchedule := time.Date(2026, 9, 4, 10, 0, 0, 0, location)
	if due := service.lastScheduledRun(afterSchedule); due.Day() != 4 || due.Hour() != 3 {
		t.Fatalf("after schedule due=%s", due)
	}
	beforeSchedule := time.Date(2026, 9, 4, 2, 0, 0, 0, location)
	if due := service.lastScheduledRun(beforeSchedule); due.Day() != 3 || due.Hour() != 3 {
		t.Fatalf("before schedule due=%s", due)
	}
}
