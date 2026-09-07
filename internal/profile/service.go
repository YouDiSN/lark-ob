package profile

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/model"
)

type Repository interface {
	PersonProfile(context.Context, string) (model.PersonProfile, error)
	UpsertPersonProfile(context.Context, model.PersonProfile) error
}

type Finder interface {
	FindPersonProfile(context.Context, string, string) (model.PersonProfile, error)
}

type request struct{ id, name string }

type Service struct {
	repo    Repository
	finder  Finder
	queue   chan request
	mu      sync.Mutex
	pending map[string]struct{}
	notify  func()
}

func New(repo Repository, finder Finder) *Service {
	return &Service{repo: repo, finder: finder, queue: make(chan request, 512), pending: map[string]struct{}{}, notify: func() {}}
}

func (s *Service) SetNotify(notify func()) {
	if notify != nil {
		s.notify = notify
	}
}

func (s *Service) Enqueue(messages []model.Message) {
	for _, message := range messages {
		if message.SenderID == "" || message.SenderName == "" {
			continue
		}
		s.mu.Lock()
		if _, exists := s.pending[message.SenderID]; exists {
			s.mu.Unlock()
			continue
		}
		s.pending[message.SenderID] = struct{}{}
		s.mu.Unlock()
		select {
		case s.queue <- request{id: message.SenderID, name: message.SenderName}:
		default:
			s.done(message.SenderID)
		}
	}
}

func (s *Service) Run(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	for worker := 0; worker < workers; worker++ {
		go s.worker(ctx)
	}
	<-ctx.Done()
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case request := <-s.queue:
			s.resolve(ctx, request)
			s.done(request.id)
		}
	}
}

func (s *Service) resolve(ctx context.Context, request request) {
	profile, err := s.repo.PersonProfile(ctx, request.id)
	if err == nil && profile.CheckedAt > time.Now().Add(-24*time.Hour).UnixMilli() {
		return
	}
	if err != nil && err != sql.ErrNoRows {
		return
	}
	resolved, err := s.finder.FindPersonProfile(ctx, request.id, request.name)
	if err != nil {
		return
	}
	resolved.ID = request.id
	if resolved.Name == "" {
		resolved.Name = request.name
	}
	resolved.CheckedAt = time.Now().UnixMilli()
	if s.repo.UpsertPersonProfile(ctx, resolved) == nil {
		s.notify()
	}
}

func (s *Service) done(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}
