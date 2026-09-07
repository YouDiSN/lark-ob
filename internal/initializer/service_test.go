package initializer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/memory"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/syncer"
)

type fakeRepo struct {
	mu       sync.Mutex
	settings map[string][]byte
	chats    []model.Chat
}

func (f *fakeRepo) Chats(context.Context) ([]model.Chat, error) { return f.chats, nil }
func (f *fakeRepo) GetJSON(_ context.Context, key string, value any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.settings[key]
	if !ok {
		return fmt.Errorf("missing")
	}
	return json.Unmarshal(raw, value)
}
func (f *fakeRepo) SetJSON(_ context.Context, key string, value any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, _ := json.Marshal(value)
	f.settings[key] = raw
	return nil
}
func (f *fakeRepo) DeleteJSON(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.settings, key)
	return nil
}

type fakeSyncer struct {
	since time.Time
	chats []model.Chat
}

func (*fakeSyncer) Sync(context.Context) error { return nil }
func (f *fakeSyncer) SyncMessagesSince(_ context.Context, chats []model.Chat, since time.Time, progress func(syncer.RangeSyncResult, int, int)) ([]syncer.RangeSyncResult, error) {
	f.since = since
	f.chats = append([]model.Chat(nil), chats...)
	if len(chats) == 0 {
		return nil, nil
	}
	results := []syncer.RangeSyncResult{{Chat: chats[0], MessageCount: 12}}
	progress(results[0], 1, 1)
	return results, nil
}

func TestRunResumesCompletedHistoryChats(t *testing.T) {
	repo := &fakeRepo{settings: map[string][]byte{}, chats: []model.Chat{{ID: "c1", Name: "已完成"}, {ID: "c2", Name: "待同步"}}}
	if err := repo.SetJSON(context.Background(), historyCheckpointKey("c1"), completion{Version: version, LookbackDays: 30}); err != nil {
		t.Fatal(err)
	}
	history := &fakeSyncer{}
	extractor := &fakeExtractor{}
	service := New(Config{LookbackDays: 30, Workers: 1}, repo, history, extractor)
	if err := service.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(history.chats) != 1 || history.chats[0].ID != "c2" {
		t.Fatalf("history chats = %#v", history.chats)
	}
	if status := service.Status(); status.HistoryDone != 2 || status.MemoryDone != 2 {
		t.Fatalf("unexpected status: %#v", status)
	}
}

type fakeExtractor struct {
	calls      int
	clearCalls int
	err        error
}

func (*fakeExtractor) Status() agent.Status { return agent.Status{Enabled: true} }
func (f *fakeExtractor) ExtractChatMemorySince(context.Context, string, time.Time) (agent.ChatMemoryResult, error) {
	f.calls++
	if f.err != nil {
		return agent.ChatMemoryResult{}, f.err
	}
	return agent.ChatMemoryResult{Claims: make([]memory.Claim, 2)}, nil
}
func (f *fakeExtractor) ClearChatMemory(context.Context, string) (agent.ChatMemoryResult, error) {
	f.clearCalls++
	return agent.ChatMemoryResult{}, f.err
}

func TestRunPersistsPerChatFailureDetails(t *testing.T) {
	repo := &fakeRepo{settings: map[string][]byte{}, chats: []model.Chat{{ID: "c1", Name: "超时群聊"}}}
	service := New(Config{LookbackDays: 30, Workers: 1}, repo, &fakeSyncer{}, &fakeExtractor{err: fmt.Errorf("context deadline exceeded")})
	if err := service.Run(context.Background()); err == nil {
		t.Fatal("expected initialization failure")
	}
	status := service.Status()
	if status.FailedChats != 1 || len(status.Failures) != 1 || status.Failures[0].ChatName != "超时群聊" || status.Failures[0].Attempts != 2 {
		t.Fatalf("unexpected failure status: %#v", status)
	}
	var persisted Failure
	if err := repo.GetJSON(context.Background(), failureKey("c1"), &persisted); err != nil || persisted.Error != "context deadline exceeded" {
		t.Fatalf("failure was not persisted: %#v, %v", persisted, err)
	}
}

func TestRunUsesConfiguredLookbackAndRunsOnce(t *testing.T) {
	repo := &fakeRepo{settings: map[string][]byte{}, chats: []model.Chat{{ID: "c1", Name: "群聊"}}}
	history := &fakeSyncer{}
	extractor := &fakeExtractor{}
	service := New(Config{LookbackDays: 30, Workers: 1}, repo, history, extractor)
	if err := service.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if days := time.Since(history.since).Hours() / 24; days < 29.9 || days > 30.1 {
		t.Fatalf("lookback days = %.2f", days)
	}
	if !service.Status().Complete || extractor.calls != 1 {
		t.Fatalf("unexpected status/calls: %#v, %d", service.Status(), extractor.calls)
	}
	second := New(Config{LookbackDays: 30, Workers: 1}, repo, history, extractor)
	if err := second.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if extractor.calls != 1 {
		t.Fatalf("completed initialization ran twice: %d", extractor.calls)
	}
}

func TestRunClearsMessageBoxMemoryWithoutExtraction(t *testing.T) {
	repo := &fakeRepo{settings: map[string][]byte{}, chats: []model.Chat{{ID: "box", Name: "消息盒子群", InMessageBox: true}}}
	extractor := &fakeExtractor{}
	service := New(Config{LookbackDays: 30, Workers: 1}, repo, &fakeSyncer{}, extractor)
	if err := service.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if extractor.calls != 0 || extractor.clearCalls != 1 {
		t.Fatalf("extract calls=%d clear calls=%d", extractor.calls, extractor.clearCalls)
	}
}
