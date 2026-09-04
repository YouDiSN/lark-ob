package syncer

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/lark"
	"github.com/youdisn/lark-ob/internal/larkcli"
	"github.com/youdisn/lark-ob/internal/model"
	"github.com/youdisn/lark-ob/internal/store"
)

type Syncer struct {
	store     *store.Store
	client    *lark.Client
	cli       *larkcli.Client
	chatLimit int
	mu        sync.RWMutex
	status    model.Status
	notify    func()
}

type HistoryState struct {
	HasMore   bool   `json:"hasMore"`
	PageToken string `json:"pageToken,omitempty"`
}

func New(s *store.Store, c *lark.Client, cli *larkcli.Client, limit int) *Syncer {
	mode := "lark"
	authMode := "oauth"
	if cli != nil && cli.Available() {
		mode = "lark-cli"
		authMode = "cli"
	}
	return &Syncer{store: s, client: c, cli: cli, chatLimit: limit, status: model.Status{Mode: mode, Configured: c.Configured() || (cli != nil && cli.Available()), AuthMode: authMode}, notify: func() {}}
}
func (s *Syncer) SetNotify(fn func()) {
	if fn != nil {
		s.notify = fn
	}
}
func (s *Syncer) Status() model.Status { s.mu.RLock(); defer s.mu.RUnlock(); return s.status }

func (s *Syncer) setStatus(fn func(*model.Status)) {
	s.mu.Lock()
	fn(&s.status)
	s.mu.Unlock()
	s.notify()
}

func (s *Syncer) token(ctx context.Context) (lark.Token, error) {
	var t lark.Token
	if err := s.store.GetJSON(ctx, "lark_token", &t); err != nil {
		if err == sql.ErrNoRows {
			return t, fmt.Errorf("Lark 尚未授权")
		}
		return t, err
	}
	if t.ExpiresAt > time.Now().Add(5*time.Minute).Unix() {
		return t, nil
	}
	if t.RefreshToken == "" {
		return t, fmt.Errorf("Lark 授权已过期，请重新登录")
	}
	refreshed, err := s.client.Refresh(ctx, t.RefreshToken)
	if err != nil {
		return t, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = t.RefreshToken
	}
	if err := s.store.SetJSON(ctx, "lark_token", refreshed); err != nil {
		return t, err
	}
	return refreshed, nil
}

func (s *Syncer) SaveToken(ctx context.Context, t lark.Token) error {
	if err := s.store.SetJSON(ctx, "lark_token", t); err != nil {
		return err
	}
	s.setStatus(func(st *model.Status) { st.Connected = true; st.Mode = "lark"; st.Error = "" })
	return nil
}

func (s *Syncer) HasToken(ctx context.Context) bool {
	ok := false
	if s.cli != nil && s.cli.Available() && s.status.Mode == "lark-cli" {
		ok = s.cli.Connected(ctx)
	} else {
		_, err := s.token(ctx)
		ok = err == nil
	}
	s.mu.Lock()
	changed := s.status.Connected != ok
	s.status.Connected = ok
	s.mu.Unlock()
	if changed {
		s.notify()
	}
	return ok
}

func (s *Syncer) Sync(ctx context.Context) error {
	s.mu.Lock()
	if s.status.Syncing {
		s.mu.Unlock()
		return nil
	}
	s.status.Syncing = true
	s.status.Error = ""
	s.mu.Unlock()
	s.notify()
	defer func() { s.setStatus(func(st *model.Status) { st.Syncing = false }) }()
	if s.cli != nil && s.cli.Available() && s.status.Mode == "lark-cli" {
		return s.syncCLI(ctx)
	}
	t, err := s.token(ctx)
	if err != nil {
		s.setStatus(func(st *model.Status) { st.Connected = false; st.Error = err.Error() })
		return err
	}
	selfID, _, _ := s.client.UserInfo(ctx, t.AccessToken)
	// The first successful run discovers every visible conversation. Later polls
	// only revisit the most recently active conversations to stay well below
	// tenant rate limits; a future active chat naturally moves into this window.
	limit := s.chatLimit
	var lastSync int64
	if s.store.GetJSON(ctx, "last_sync", &lastSync) == nil && lastSync > 0 {
		limit = 20
	}
	chats, err := s.client.Chats(ctx, t.AccessToken, limit)
	if err != nil {
		s.fail(err)
		return err
	}
	for _, c := range chats {
		if c.Name == "" {
			if c.Type == "p2p" {
				c.Name = "单聊"
			} else {
				c.Name = "未命名群聊"
			}
		}
		if err := s.store.UpsertChat(ctx, c); err != nil {
			s.fail(err)
			return err
		}
		msgs, next, hasMore, msgErr := s.client.MessagesPage(ctx, t.AccessToken, c.ID, "")
		if msgErr != nil {
			continue
		}
		for _, m := range msgs {
			m.IsSelf = selfID != "" && m.SenderID == selfID
			if m.SenderName == "" {
				if m.IsSelf {
					m.SenderName = "我"
				} else {
					m.SenderName = "成员"
				}
			}
			_ = s.store.UpsertMessage(ctx, m)
		}
		var existing HistoryState
		if s.store.GetJSON(ctx, "history:"+c.ID, &existing) != nil {
			_ = s.store.SetJSON(ctx, "history:"+c.ID, HistoryState{HasMore: hasMore, PageToken: next})
		}
	}
	now := time.Now().UnixMilli()
	s.setStatus(func(st *model.Status) { st.Connected = true; st.LastSync = now; st.Error = "" })
	_ = s.store.SetJSON(ctx, "last_sync", now)
	return nil
}

func (s *Syncer) syncCLI(ctx context.Context) error {
	selfID, _, err := s.cli.Self(ctx)
	if err != nil {
		s.fail(err)
		return err
	}
	limit := s.chatLimit
	var lastSync int64
	if s.store.GetJSON(ctx, "last_sync", &lastSync) == nil && lastSync > 0 {
		limit = 20
	}
	chats, err := s.cli.Chats(ctx, limit)
	if err != nil {
		s.fail(err)
		return err
	}
	for _, c := range chats {
		if c.Name == "" {
			if c.Type == "p2p" {
				c.Name = "单聊"
			} else {
				c.Name = "未命名群聊"
			}
		}
		if err := s.store.UpsertChat(ctx, c); err != nil {
			return err
		}
		msgs, next, hasMore, msgErr := s.cli.MessagesPage(ctx, c.ID, "")
		if msgErr != nil {
			continue
		}
		for _, m := range msgs {
			m.IsSelf = m.SenderID == selfID
			if m.SenderName == "" {
				if m.IsSelf {
					m.SenderName = "我"
				} else {
					m.SenderName = "成员"
				}
			}
			_ = s.store.UpsertMessage(ctx, m)
		}
		var existing HistoryState
		if s.store.GetJSON(ctx, "history:"+c.ID, &existing) != nil {
			_ = s.store.SetJSON(ctx, "history:"+c.ID, HistoryState{HasMore: hasMore, PageToken: next})
		}
	}
	now := time.Now().UnixMilli()
	s.setStatus(func(st *model.Status) { st.Connected = true; st.LastSync = now; st.Error = "" })
	_ = s.store.SetJSON(ctx, "last_sync", now)
	return nil
}

func (s *Syncer) History(ctx context.Context, chatID string) HistoryState {
	var state HistoryState
	_ = s.store.GetJSON(ctx, "history:"+chatID, &state)
	return state
}

func (s *Syncer) LoadOlder(ctx context.Context, chatID string) (int, HistoryState, error) {
	state := s.History(ctx, chatID)
	if !state.HasMore || state.PageToken == "" {
		return 0, state, nil
	}
	if s.cli != nil && s.cli.Available() && s.status.Mode == "lark-cli" {
		selfID, _, err := s.cli.Self(ctx)
		if err != nil {
			return 0, state, err
		}
		msgs, next, hasMore, err := s.cli.MessagesPage(ctx, chatID, state.PageToken)
		if err != nil {
			return 0, state, err
		}
		for _, m := range msgs {
			m.IsSelf = m.SenderID == selfID
			if m.SenderName == "" {
				if m.IsSelf {
					m.SenderName = "我"
				} else {
					m.SenderName = "成员"
				}
			}
			if err := s.store.UpsertMessage(ctx, m); err != nil {
				return 0, state, err
			}
		}
		state = HistoryState{HasMore: hasMore, PageToken: next}
		if err := s.store.SetJSON(ctx, "history:"+chatID, state); err != nil {
			return 0, state, err
		}
		s.notify()
		return len(msgs), state, nil
	}
	t, err := s.token(ctx)
	if err != nil {
		return 0, state, err
	}
	selfID, _, _ := s.client.UserInfo(ctx, t.AccessToken)
	msgs, next, hasMore, err := s.client.MessagesPage(ctx, t.AccessToken, chatID, state.PageToken)
	if err != nil {
		return 0, state, err
	}
	for _, m := range msgs {
		m.IsSelf = selfID != "" && m.SenderID == selfID
		if m.SenderName == "" {
			if m.IsSelf {
				m.SenderName = "我"
			} else {
				m.SenderName = "成员"
			}
		}
		if err := s.store.UpsertMessage(ctx, m); err != nil {
			return 0, state, err
		}
	}
	state = HistoryState{HasMore: hasMore, PageToken: next}
	if err := s.store.SetJSON(ctx, "history:"+chatID, state); err != nil {
		return 0, state, err
	}
	s.notify()
	return len(msgs), state, nil
}

func (s *Syncer) fail(err error) { s.setStatus(func(st *model.Status) { st.Error = err.Error() }) }

func (s *Syncer) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.HasToken(ctx) {
				go s.Sync(context.Background())
			}
		}
	}
}
