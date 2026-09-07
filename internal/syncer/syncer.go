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
	store       *store.Store
	client      *lark.Client
	cli         *larkcli.Client
	chatLimit   int
	operationMu sync.Mutex
	refreshMu   sync.Mutex
	identityMu  sync.RWMutex
	selfID      string
	selfName    string
	mu          sync.RWMutex
	status      model.Status
	notify      func()
	profiles    interface{ Enqueue([]model.Message) }
	contexts    interface{ EnqueueChatContext(string) }
}

type HistoryState struct {
	HasMore   bool   `json:"hasMore"`
	PageToken string `json:"pageToken,omitempty"`
}

type RangeSyncResult struct {
	Chat         model.Chat
	MessageCount int
	Err          error
}

type ChatSyncResult struct {
	ChatID      string `json:"chatId"`
	Fetched     int    `json:"fetched"`
	NewMessages int    `json:"newMessages"`
	LastTime    int64  `json:"lastTime"`
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
func (s *Syncer) SetProfileQueue(queue interface{ Enqueue([]model.Message) }) { s.profiles = queue }
func (s *Syncer) SetChatContextQueue(queue interface{ EnqueueChatContext(string) }) {
	s.contexts = queue
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

func (s *Syncer) cliSelf(ctx context.Context) (string, string, error) {
	s.identityMu.RLock()
	id, name := s.selfID, s.selfName
	s.identityMu.RUnlock()
	if id != "" {
		return id, name, nil
	}
	id, name, err := s.cli.Self(ctx)
	if err != nil {
		return "", "", err
	}
	s.identityMu.Lock()
	s.selfID, s.selfName = id, name
	s.identityMu.Unlock()
	return id, name, nil
}

func (s *Syncer) HasToken(ctx context.Context) bool {
	ok := false
	if s.cli != nil && s.cli.Available() && s.status.Mode == "lark-cli" {
		status, err := s.cli.Status(ctx)
		ok = err == nil && status.Identity == "user"
		if ok && status.UserOpenID != "" {
			var repairedIdentity string
			const repairKey = "cli_message_identity_repair"
			if s.store.GetJSON(ctx, repairKey, &repairedIdentity) != nil || repairedIdentity != status.UserOpenID {
				if repairErr := s.store.ReclassifyMessages(ctx, status.UserOpenID); repairErr == nil {
					_ = s.store.SetJSON(ctx, repairKey, status.UserOpenID)
				}
			}
		}
		s.identityMu.Lock()
		if ok {
			s.selfID, s.selfName = status.UserOpenID, status.UserName
		} else {
			s.selfID, s.selfName = "", ""
		}
		s.identityMu.Unlock()
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
	if !s.operationMu.TryLock() {
		return nil
	}
	defer s.operationMu.Unlock()
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
	limit := s.pollLimit(ctx)
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
		if _, err := s.storeMessages(ctx, msgs, selfID); err != nil {
			s.fail(err)
			return err
		}
		var existing HistoryState
		if s.store.GetJSON(ctx, "history:"+c.ID, &existing) != nil {
			_ = s.store.SetJSON(ctx, "history:"+c.ID, HistoryState{HasMore: hasMore, PageToken: next})
		}
	}
	now := time.Now().UnixMilli()
	s.setStatus(func(st *model.Status) { st.Connected = true; st.LastSync = now; st.Error = "" })
	_ = s.store.SetJSON(ctx, "last_sync", now)
	_ = s.store.SetJSON(ctx, "timestamp_repair_version", 1)
	return nil
}

func (s *Syncer) syncCLI(ctx context.Context) error {
	selfID, _, err := s.cliSelf(ctx)
	if err != nil {
		s.fail(err)
		return err
	}
	if err := s.repairCLITimestamps(ctx); err != nil {
		s.fail(err)
		return err
	}
	limit := s.pollLimit(ctx)
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
		if _, err := s.storeMessages(ctx, msgs, selfID); err != nil {
			s.fail(err)
			return err
		}
		var existing HistoryState
		if s.store.GetJSON(ctx, "history:"+c.ID, &existing) != nil {
			_ = s.store.SetJSON(ctx, "history:"+c.ID, HistoryState{HasMore: hasMore, PageToken: next})
		}
	}
	now := time.Now().UnixMilli()
	s.setStatus(func(st *model.Status) { st.Connected = true; st.LastSync = now; st.Error = "" })
	_ = s.store.SetJSON(ctx, "last_sync", now)
	_ = s.store.SetJSON(ctx, "timestamp_repair_version", 1)
	return nil
}

func (s *Syncer) repairCLITimestamps(ctx context.Context) error {
	const repairVersion = 1
	var completedVersion int
	if s.store.GetJSON(ctx, "cli_timestamp_mget_repair_version", &completedVersion) == nil && completedVersion >= repairVersion {
		return nil
	}
	ids, err := s.store.MessageIDsWithoutTimestamp(ctx)
	if err != nil {
		return err
	}
	for start := 0; start < len(ids); start += 50 {
		end := start + 50
		if end > len(ids) {
			end = len(ids)
		}
		messages, err := s.cli.MessagesByIDs(ctx, ids[start:end])
		if err != nil {
			return err
		}
		if _, err := s.store.RepairMessageTimestamps(ctx, messages); err != nil {
			return err
		}
	}
	return s.store.SetJSON(ctx, "cli_timestamp_mget_repair_version", repairVersion)
}

// SyncMessagesSince backfills a bounded history window for every discovered
// conversation. It intentionally does not mutate the manual "load older"
// cursor, whose range extends beyond the memory window.
func (s *Syncer) SyncMessagesSince(ctx context.Context, chats []model.Chat, since time.Time, progress func(RangeSyncResult, int, int)) ([]RangeSyncResult, error) {
	s.operationMu.Lock()
	defer s.operationMu.Unlock()
	s.mu.Lock()
	s.status.Syncing = true
	s.mu.Unlock()
	s.notify()
	defer s.setStatus(func(status *model.Status) { status.Syncing = false })
	if progress == nil {
		progress = func(RangeSyncResult, int, int) {}
	}
	if s.cli != nil && s.cli.Available() && s.Status().Mode == "lark-cli" {
		selfID, _, err := s.cliSelf(ctx)
		if err != nil {
			return nil, err
		}
		return s.syncChatsSince(ctx, chats, 2, func(chat model.Chat) (int, error) {
			return s.syncCLIChatSince(ctx, chat.ID, since, selfID)
		}, progress), nil
	}
	token, err := s.token(ctx)
	if err != nil {
		return nil, err
	}
	selfID, _, _ := s.client.UserInfo(ctx, token.AccessToken)
	return s.syncChatsSince(ctx, chats, 2, func(chat model.Chat) (int, error) {
		return s.syncLarkChatSince(ctx, token.AccessToken, chat.ID, since, selfID)
	}, progress), nil
}

func (s *Syncer) syncChatsSince(ctx context.Context, chats []model.Chat, workers int, syncChat func(model.Chat) (int, error), progress func(RangeSyncResult, int, int)) []RangeSyncResult {
	if len(chats) == 0 {
		return []RangeSyncResult{}
	}
	if workers > len(chats) {
		workers = len(chats)
	}
	jobs := make(chan model.Chat, len(chats))
	completed := make(chan RangeSyncResult, len(chats))
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for chat := range jobs {
				if ctx.Err() != nil {
					return
				}
				count, err := syncChat(chat)
				completed <- RangeSyncResult{Chat: chat, MessageCount: count, Err: err}
			}
		}()
	}
	for _, chat := range chats {
		jobs <- chat
	}
	close(jobs)
	go func() { group.Wait(); close(completed) }()
	results := make([]RangeSyncResult, 0, len(chats))
	for result := range completed {
		results = append(results, result)
		progress(result, len(results), len(chats))
	}
	return results
}

func (s *Syncer) syncCLIChatSince(ctx context.Context, chatID string, since time.Time, selfID string) (int, error) {
	messages, err := s.cli.MessagesSince(ctx, chatID, since)
	if err != nil {
		return 0, err
	}
	return s.storeMessages(ctx, messages, selfID)
}

func (s *Syncer) syncLarkChatSince(ctx context.Context, token, chatID string, since time.Time, selfID string) (int, error) {
	return s.syncChatPages(ctx, chatID, func(pageToken string) ([]model.Message, string, bool, error) {
		return s.client.MessagesPageSince(ctx, token, chatID, pageToken, since)
	}, selfID)
}

func (s *Syncer) syncChatPages(ctx context.Context, chatID string, fetch func(string) ([]model.Message, string, bool, error), selfID string) (int, error) {
	pageToken := ""
	total := 0
	for page := 0; page < 1000; page++ {
		messages, next, hasMore, err := fetch(pageToken)
		if err != nil {
			return total, err
		}
		stored, err := s.storeMessages(ctx, messages, selfID)
		if err != nil {
			return total, err
		}
		total += stored
		if !hasMore || next == "" {
			s.notify()
			return total, nil
		}
		if next == pageToken {
			return total, fmt.Errorf("会话 %s 的消息游标没有前进", chatID)
		}
		pageToken = next
	}
	return total, fmt.Errorf("会话 %s 在 1000 页后仍未完成最近消息同步", chatID)
}

func (s *Syncer) storeMessages(ctx context.Context, messages []model.Message, selfID string) (int, error) {
	newMessages := 0
	for index := range messages {
		message := &messages[index]
		message.IsSelf = selfID != "" && message.SenderID == selfID
		message.MentionsSelf = false
		for _, mention := range message.Mentions {
			if selfID != "" && mention.ID == selfID {
				message.MentionsSelf = true
				break
			}
		}
		if message.SenderName == "" {
			if message.IsSelf {
				message.SenderName = "我"
			} else {
				message.SenderName = "成员"
			}
		}
		inserted, err := s.store.UpsertMessageWithResult(ctx, *message)
		if err != nil {
			return 0, err
		}
		if inserted {
			newMessages++
		}
	}
	if s.profiles != nil {
		s.profiles.Enqueue(messages)
	}
	if newMessages > 0 && s.contexts != nil && len(messages) > 0 {
		s.contexts.EnqueueChatContext(messages[0].ChatID)
	}
	return len(messages), nil
}

// SyncChat refreshes one conversation without waiting for the periodic
// all-chat poll. Its lock is intentionally separate from the long-running
// initialization/global sync lock; message upserts are idempotent.
func (s *Syncer) SyncChat(ctx context.Context, chatID string) (ChatSyncResult, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	chat, err := s.store.Chat(ctx, chatID)
	if err != nil {
		return ChatSyncResult{}, err
	}
	beforeTime, beforePosition := chat.LastTime, chat.LastPosition
	var messages []model.Message
	if s.cli != nil && s.cli.Available() && s.Status().Mode == "lark-cli" {
		selfID, _, selfErr := s.cliSelf(ctx)
		if selfErr != nil {
			return ChatSyncResult{}, selfErr
		}
		messages, _, _, err = s.cli.MessagesPage(ctx, chatID, "")
		if err == nil {
			_, err = s.storeMessages(ctx, messages, selfID)
		}
	} else {
		token, tokenErr := s.token(ctx)
		if tokenErr != nil {
			return ChatSyncResult{}, tokenErr
		}
		selfID, _, _ := s.client.UserInfo(ctx, token.AccessToken)
		messages, _, _, err = s.client.MessagesPage(ctx, token.AccessToken, chatID, "")
		if err == nil {
			_, err = s.storeMessages(ctx, messages, selfID)
		}
	}
	if err != nil {
		return ChatSyncResult{}, err
	}
	result := ChatSyncResult{ChatID: chatID, Fetched: len(messages), LastTime: beforeTime}
	for _, message := range messages {
		if !message.IsSelf && (message.CreatedAt > beforeTime || (message.CreatedAt == beforeTime && message.Position > beforePosition)) {
			result.NewMessages++
		}
		if message.CreatedAt > result.LastTime {
			result.LastTime = message.CreatedAt
		}
	}
	now := time.Now().UnixMilli()
	s.setStatus(func(status *model.Status) {
		status.Connected = true
		status.LastSync = now
		status.Error = ""
	})
	_ = s.store.SetJSON(ctx, "last_sync", now)
	return result, nil
}

// BackfillCLIPositions performs a one-time bounded re-read so existing rows
// gain Feishu's canonical message_position without rebuilding user memories.
func (s *Syncer) BackfillCLIPositions(ctx context.Context, since time.Time) error {
	if s.cli == nil || !s.cli.Available() || s.Status().Mode != "lark-cli" {
		return nil
	}
	const version = 1
	var completed int
	if s.store.GetJSON(ctx, "cli_message_position_backfill_version", &completed) == nil && completed >= version {
		return nil
	}
	chats, err := s.store.ChatsNeedingMessagePositions(ctx, since.UnixMilli())
	if err != nil {
		return err
	}
	results, err := s.SyncMessagesSince(ctx, chats, since, nil)
	if err != nil {
		return err
	}
	failed := 0
	for _, result := range results {
		if result.Err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d 个会话的消息顺序回填失败", failed)
	}
	return s.store.SetJSON(ctx, "cli_message_position_backfill_version", version)
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
		selfID, _, err := s.cliSelf(ctx)
		if err != nil {
			return 0, state, err
		}
		msgs, next, hasMore, err := s.cli.MessagesPage(ctx, chatID, state.PageToken)
		if err != nil {
			return 0, state, err
		}
		if _, err := s.storeMessages(ctx, msgs, selfID); err != nil {
			return 0, state, err
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
	if _, err := s.storeMessages(ctx, msgs, selfID); err != nil {
		return 0, state, err
	}
	state = HistoryState{HasMore: hasMore, PageToken: next}
	if err := s.store.SetJSON(ctx, "history:"+chatID, state); err != nil {
		return 0, state, err
	}
	s.notify()
	return len(msgs), state, nil
}

func (s *Syncer) fail(err error) { s.setStatus(func(st *model.Status) { st.Error = err.Error() }) }

func (s *Syncer) pollLimit(ctx context.Context) int {
	limit := s.chatLimit
	var lastSync int64
	if s.store.GetJSON(ctx, "last_sync", &lastSync) == nil && lastSync > 0 {
		var repairVersion int
		repairDone := s.store.GetJSON(ctx, "timestamp_repair_version", &repairVersion) == nil && repairVersion >= 1
		if repairDone || !s.store.HasMessagesWithoutTimestamp(ctx) {
			limit = 20
		}
	}
	return limit
}

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
