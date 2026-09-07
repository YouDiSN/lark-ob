package maintenance

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/agent"
	"github.com/youdisn/lark-ob/internal/impression"
	"github.com/youdisn/lark-ob/internal/model"
)

const checkpointKey = "daily_profile_maintenance"

type Config struct {
	LookbackDays int
	Workers      int
	Hour         int
	Minute       int
	Location     *time.Location
}

type Repository interface {
	Chats(context.Context) ([]model.Chat, error)
	GetJSON(context.Context, string, any) error
	SetJSON(context.Context, string, any) error
}

type Syncer interface {
	Sync(context.Context) error
}

type MemoryExtractor interface {
	ExtractChatMemorySince(context.Context, string, time.Time) (agent.ChatMemoryResult, error)
	ClearChatMemory(context.Context, string) (agent.ChatMemoryResult, error)
}

type ImpressionUpdater interface {
	Update(context.Context, time.Time, time.Time) (impression.UpdateResult, error)
}

type Status struct {
	Running          bool   `json:"running"`
	Phase            string `json:"phase"`
	LookbackDays     int    `json:"lookbackDays"`
	Schedule         string `json:"schedule"`
	Timezone         string `json:"timezone"`
	LastStartedAt    int64  `json:"lastStartedAt,omitempty"`
	LastSucceededAt  int64  `json:"lastSucceededAt,omitempty"`
	NextRunAt        int64  `json:"nextRunAt,omitempty"`
	UpdatedChats     int    `json:"updatedChats"`
	ImpressionsSaved int    `json:"impressionsSaved"`
	StylesSaved      int    `json:"stylesSaved"`
	Error            string `json:"error,omitempty"`
}

type checkpoint struct {
	LastSucceededAt int64 `json:"lastSucceededAt"`
}

type Service struct {
	config      Config
	repo        Repository
	syncer      Syncer
	extractor   MemoryExtractor
	impressions ImpressionUpdater
	mu          sync.RWMutex
	status      Status
	notify      func()
}

func New(config Config, repo Repository, syncer Syncer, extractor MemoryExtractor, impressions ImpressionUpdater) *Service {
	if config.LookbackDays <= 0 {
		config.LookbackDays = 30
	}
	if config.Workers <= 0 {
		config.Workers = 2
	}
	if config.Hour < 0 || config.Hour > 23 {
		config.Hour = 3
	}
	if config.Minute < 0 || config.Minute > 59 {
		config.Minute = 0
	}
	if config.Location == nil {
		config.Location = time.UTC
	}
	schedule := fmt.Sprintf("%02d:%02d", config.Hour, config.Minute)
	return &Service{config: config, repo: repo, syncer: syncer, extractor: extractor, impressions: impressions,
		status: Status{Phase: "waiting", LookbackDays: config.LookbackDays, Schedule: schedule, Timezone: config.Location.String()}, notify: func() {}}
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
	s.mu.Unlock()
	s.notify()
}

// Run performs the one-time portrait bootstrap when no successful checkpoint
// exists, then waits for the configured wall-clock time each day.
func (s *Service) Run(ctx context.Context) {
	var state checkpoint
	hasCheckpoint := s.repo.GetJSON(ctx, checkpointKey, &state) == nil && state.LastSucceededAt > 0
	if !hasCheckpoint {
		if err := s.run(ctx, true); err != nil && ctx.Err() == nil {
			s.update(func(status *Status) { status.Error = err.Error(); status.Phase = "error" })
		}
	} else {
		s.update(func(status *Status) { status.LastSucceededAt = state.LastSucceededAt })
		// If the application was stopped at the configured wall-clock time,
		// catch up once on startup instead of silently skipping an entire day.
		lastDue := s.lastScheduledRun(time.Now())
		if state.LastSucceededAt < lastDue.UnixMilli() {
			if err := s.run(ctx, false); err != nil && ctx.Err() == nil {
				s.update(func(status *Status) { status.Error = err.Error(); status.Phase = "error" })
			}
		}
	}
	for ctx.Err() == nil {
		next := s.nextRun(time.Now())
		s.update(func(status *Status) {
			status.NextRunAt = next.UnixMilli()
			if !status.Running {
				status.Phase = "waiting"
			}
		})
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := s.run(ctx, false); err != nil && ctx.Err() == nil {
				s.update(func(status *Status) { status.Error = err.Error(); status.Phase = "error" })
			}
		}
	}
}

func (s *Service) RunNow(ctx context.Context) error { return s.run(ctx, false) }

func (s *Service) run(ctx context.Context, bootstrap bool) error {
	s.mu.Lock()
	if s.status.Running {
		s.mu.Unlock()
		return nil
	}
	s.status.Running = true
	s.status.Phase = "sync"
	s.status.Error = ""
	s.status.LastStartedAt = time.Now().UnixMilli()
	s.status.UpdatedChats = 0
	s.status.ImpressionsSaved = 0
	s.status.StylesSaved = 0
	s.mu.Unlock()
	s.notify()
	defer s.update(func(status *Status) { status.Running = false })

	var previous checkpoint
	_ = s.repo.GetJSON(ctx, checkpointKey, &previous)
	changedSince := time.UnixMilli(previous.LastSucceededAt)
	windowStart := time.Now().AddDate(0, 0, -s.config.LookbackDays)
	if !bootstrap {
		if err := s.syncer.Sync(ctx); err != nil {
			return fmt.Errorf("每日消息同步失败: %w", err)
		}
		chats, err := s.repo.Chats(ctx)
		if err != nil {
			return err
		}
		changed := make([]model.Chat, 0, len(chats))
		messageBoxChats := make([]model.Chat, 0)
		for _, chat := range chats {
			if chat.InMessageBox {
				messageBoxChats = append(messageBoxChats, chat)
				continue
			}
			if chat.LastTime > previous.LastSucceededAt {
				changed = append(changed, chat)
			}
		}
		s.update(func(status *Status) { status.Phase = "memory" })
		if err := s.clearMemories(ctx, messageBoxChats); err != nil {
			return err
		}
		if err := s.refreshMemories(ctx, changed, windowStart); err != nil {
			return err
		}
		s.update(func(status *Status) { status.UpdatedChats = len(changed) })
	}

	s.update(func(status *Status) { status.Phase = "impression" })
	result, err := s.impressions.Update(ctx, windowStart, changedSince)
	s.update(func(status *Status) {
		status.ImpressionsSaved = result.ImpressionsSaved
		status.StylesSaved = result.StylesSaved
	})
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if err := s.repo.SetJSON(ctx, checkpointKey, checkpoint{LastSucceededAt: now}); err != nil {
		return err
	}
	s.update(func(status *Status) {
		status.Phase = "waiting"
		status.LastSucceededAt = now
		status.Error = ""
	})
	return nil
}

func (s *Service) clearMemories(ctx context.Context, chats []model.Chat) error {
	for _, chat := range chats {
		if _, err := s.extractor.ClearChatMemory(ctx, chat.ID); err != nil {
			return fmt.Errorf("清理消息盒子会话 %s 的记忆失败: %w", chat.Name, err)
		}
	}
	return nil
}

func (s *Service) refreshMemories(ctx context.Context, chats []model.Chat, since time.Time) error {
	if len(chats) == 0 {
		return nil
	}
	jobs := make(chan model.Chat)
	errors := make(chan error, len(chats))
	var group sync.WaitGroup
	for worker := 0; worker < s.config.Workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for chat := range jobs {
				if _, err := s.extractor.ExtractChatMemorySince(ctx, chat.ID, since); err != nil {
					errors <- fmt.Errorf("%s: %w", chat.Name, err)
				}
			}
		}()
	}
	for _, chat := range chats {
		jobs <- chat
	}
	close(jobs)
	group.Wait()
	close(errors)
	var count int
	for range errors {
		count++
	}
	if count > 0 {
		return fmt.Errorf("%d 个会话的每日记忆更新失败，已保留旧结果", count)
	}
	return nil
}

func (s *Service) nextRun(now time.Time) time.Time {
	local := now.In(s.config.Location)
	next := time.Date(local.Year(), local.Month(), local.Day(), s.config.Hour, s.config.Minute, 0, 0, s.config.Location)
	if !next.After(local) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (s *Service) lastScheduledRun(now time.Time) time.Time {
	local := now.In(s.config.Location)
	due := time.Date(local.Year(), local.Month(), local.Day(), s.config.Hour, s.config.Minute, 0, 0, s.config.Location)
	if due.After(local) {
		due = due.AddDate(0, 0, -1)
	}
	return due
}
