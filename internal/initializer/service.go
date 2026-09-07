package initializer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/syncer"
)

const version = 1

type Config struct {
	LookbackDays int
	Workers      int
}

type Repository interface {
	Chats(context.Context) ([]model.Chat, error)
	GetJSON(context.Context, string, any) error
	SetJSON(context.Context, string, any) error
	DeleteJSON(context.Context, string) error
}

type HistorySyncer interface {
	Sync(context.Context) error
	SyncMessagesSince(context.Context, []model.Chat, time.Time, func(syncer.RangeSyncResult, int, int)) ([]syncer.RangeSyncResult, error)
}

type MemoryExtractor interface {
	Status() agent.Status
	ExtractChatMemorySince(context.Context, string, time.Time) (agent.ChatMemoryResult, error)
	ClearChatMemory(context.Context, string) (agent.ChatMemoryResult, error)
}

type Status struct {
	Running      bool      `json:"running"`
	Complete     bool      `json:"complete"`
	Phase        string    `json:"phase"`
	LookbackDays int       `json:"lookbackDays"`
	TotalChats   int       `json:"totalChats"`
	HistoryDone  int       `json:"historyDone"`
	MemoryDone   int       `json:"memoryDone"`
	MessageCount int       `json:"messageCount"`
	MemoryClaims int       `json:"memoryClaims"`
	FailedChats  int       `json:"failedChats"`
	CurrentChat  string    `json:"currentChat,omitempty"`
	StartedAt    int64     `json:"startedAt,omitempty"`
	UpdatedAt    int64     `json:"updatedAt,omitempty"`
	CompletedAt  int64     `json:"completedAt,omitempty"`
	Error        string    `json:"error,omitempty"`
	Failures     []Failure `json:"failures,omitempty"`
}

type Failure struct {
	ChatID     string `json:"chatId"`
	ChatName   string `json:"chatName"`
	Stage      string `json:"stage"`
	Error      string `json:"error"`
	Attempts   int    `json:"attempts"`
	DurationMS int64  `json:"durationMs,omitempty"`
	FailedAt   int64  `json:"failedAt"`
}

type completion struct {
	Version      int   `json:"version"`
	LookbackDays int   `json:"lookbackDays"`
	CompletedAt  int64 `json:"completedAt"`
}

type Service struct {
	config    Config
	repo      Repository
	syncer    HistorySyncer
	extractor MemoryExtractor
	mu        sync.RWMutex
	status    Status
	notify    func()
}

func New(config Config, repo Repository, historySyncer HistorySyncer, extractor MemoryExtractor) *Service {
	if config.LookbackDays <= 0 {
		config.LookbackDays = 30
	}
	if config.Workers <= 0 {
		config.Workers = 2
	}
	return &Service{config: config, repo: repo, syncer: historySyncer, extractor: extractor,
		status: Status{Phase: "pending", LookbackDays: config.LookbackDays}, notify: func() {}}
}

func (s *Service) SetNotify(notify func()) {
	if notify != nil {
		s.notify = notify
	}
}

func (s *Service) Status() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *Service) update(change func(*Status)) {
	s.mu.Lock()
	change(&s.status)
	s.status.UpdatedAt = time.Now().UnixMilli()
	s.mu.Unlock()
	s.notify()
}

func (s *Service) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return nil
	}
	var done completion
	if s.repo.GetJSON(ctx, "memory_initialization", &done) == nil && done.Version >= version && done.LookbackDays == s.config.LookbackDays {
		s.status.Complete = true
		s.status.Phase = "complete"
		s.status.CompletedAt = done.CompletedAt
		s.mu.Unlock()
		return nil
	}
	s.status = Status{Running: true, Phase: "discovering", LookbackDays: s.config.LookbackDays, StartedAt: time.Now().UnixMilli()}
	s.mu.Unlock()
	s.notify()
	defer s.update(func(status *Status) { status.Running = false })

	if s.extractor == nil || !s.extractor.Status().Enabled {
		err := fmt.Errorf("记忆初始化需要已启用的 Agent")
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}
	if err := s.syncer.Sync(ctx); err != nil {
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}
	chats, err := s.repo.Chats(ctx)
	if err != nil {
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -s.config.LookbackDays)
	pendingHistory := make([]model.Chat, 0, len(chats))
	completedHistory := 0
	for _, chat := range chats {
		var chatDone completion
		if s.repo.GetJSON(ctx, historyCheckpointKey(chat.ID), &chatDone) == nil && chatDone.Version >= version && chatDone.LookbackDays == s.config.LookbackDays {
			completedHistory++
			continue
		}
		pendingHistory = append(pendingHistory, chat)
	}
	s.update(func(status *Status) {
		status.Phase = "history"
		status.TotalChats = len(chats)
		status.HistoryDone = completedHistory
		status.Error = ""
	})
	historyFailed := make(map[string]bool)
	_, err = s.syncer.SyncMessagesSince(ctx, pendingHistory, cutoff, func(result syncer.RangeSyncResult, done, _ int) {
		resultErr := result.Err
		if resultErr == nil {
			resultErr = s.repo.SetJSON(ctx, historyCheckpointKey(result.Chat.ID), completion{
				Version: version, LookbackDays: s.config.LookbackDays, CompletedAt: time.Now().UnixMilli(),
			})
		}
		if resultErr != nil {
			historyFailed[result.Chat.ID] = true
			s.recordFailure(ctx, Failure{ChatID: result.Chat.ID, ChatName: result.Chat.Name, Stage: "history", Error: resultErr.Error(), Attempts: 1, FailedAt: time.Now().UnixMilli()})
		} else {
			s.clearFailure(ctx, result.Chat.ID)
		}
		s.update(func(status *Status) {
			status.HistoryDone = completedHistory + done
			status.CurrentChat = result.Chat.Name
			status.MessageCount += result.MessageCount
		})
	})
	if err != nil {
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}

	s.update(func(status *Status) { status.Phase = "memory"; status.CurrentChat = "" })
	type job struct{ chat model.Chat }
	jobs := make(chan job, len(chats))
	var workers sync.WaitGroup
	var failuresMu sync.Mutex
	failures := 0
	for worker := 0; worker < s.config.Workers; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				if ctx.Err() != nil {
					return
				}
				key := "memory_initialization_chat:" + item.chat.ID
				var chatDone completion
				if s.repo.GetJSON(ctx, key, &chatDone) == nil && chatDone.Version >= version && chatDone.LookbackDays == s.config.LookbackDays {
					s.update(func(status *Status) { status.MemoryDone++ })
					continue
				}
				started := time.Now()
				var result agent.ChatMemoryResult
				var extractErr error
				if item.chat.InMessageBox {
					result, extractErr = s.extractor.ClearChatMemory(ctx, item.chat.ID)
				} else {
					result, extractErr = s.extractor.ExtractChatMemorySince(ctx, item.chat.ID, cutoff)
				}
				if extractErr == nil {
					now := time.Now().UnixMilli()
					extractErr = s.repo.SetJSON(ctx, key, completion{Version: version, LookbackDays: s.config.LookbackDays, CompletedAt: now})
				}
				s.update(func(status *Status) {
					status.MemoryDone++
					status.CurrentChat = item.chat.Name
					status.MemoryClaims += len(result.Claims)
				})
				if extractErr != nil {
					s.recordFailure(ctx, Failure{ChatID: item.chat.ID, ChatName: item.chat.Name, Stage: "memory", Error: extractErr.Error(), Attempts: 2, DurationMS: time.Since(started).Milliseconds(), FailedAt: time.Now().UnixMilli()})
					failuresMu.Lock()
					failures++
					failuresMu.Unlock()
				} else {
					s.clearFailure(ctx, item.chat.ID)
				}
			}
		}()
	}
	for _, chat := range chats {
		if !historyFailed[chat.ID] {
			jobs <- job{chat: chat}
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}
	if failures > 0 || s.Status().FailedChats > 0 {
		err := fmt.Errorf("%d 个会话初始化失败，将在下次启动时重试", s.Status().FailedChats)
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}
	now := time.Now().UnixMilli()
	if err := s.repo.SetJSON(ctx, "memory_initialization", completion{Version: version, LookbackDays: s.config.LookbackDays, CompletedAt: now}); err != nil {
		s.update(func(status *Status) { status.Phase = "error"; status.Error = err.Error() })
		return err
	}
	s.update(func(status *Status) {
		status.Phase = "complete"
		status.Complete = true
		status.CurrentChat = ""
		status.CompletedAt = now
	})
	return nil
}

func historyCheckpointKey(chatID string) string {
	return "memory_initialization_history:" + chatID
}

func failureKey(chatID string) string {
	return "memory_initialization_failure:" + chatID
}

func (s *Service) recordFailure(ctx context.Context, failure Failure) {
	if err := s.repo.SetJSON(ctx, failureKey(failure.ChatID), failure); err != nil {
		log.Printf("保存初始化失败详情失败: chat=%q id=%s err=%v", failure.ChatName, failure.ChatID, err)
	}
	log.Printf("会话初始化失败: stage=%s chat=%q id=%s attempts=%d duration=%dms err=%v", failure.Stage, failure.ChatName, failure.ChatID, failure.Attempts, failure.DurationMS, failure.Error)
	s.update(func(status *Status) {
		filtered := status.Failures[:0]
		for _, existing := range status.Failures {
			if existing.ChatID != failure.ChatID {
				filtered = append(filtered, existing)
			}
		}
		status.Failures = append(filtered, failure)
		status.FailedChats = len(status.Failures)
	})
}

func (s *Service) clearFailure(ctx context.Context, chatID string) {
	if err := s.repo.DeleteJSON(ctx, failureKey(chatID)); err != nil {
		log.Printf("清理初始化失败详情失败: chat_id=%s err=%v", chatID, err)
	}
	s.update(func(status *Status) {
		filtered := status.Failures[:0]
		for _, existing := range status.Failures {
			if existing.ChatID != chatID {
				filtered = append(filtered, existing)
			}
		}
		status.Failures = filtered
		status.FailedChats = len(filtered)
	})
}
