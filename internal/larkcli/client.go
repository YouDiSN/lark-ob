package larkcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/youdisn/lark-ob/internal/model"
)

type Client struct {
	path                string
	profileScopeMu      sync.Mutex
	profileScopeChecked time.Time
	profileBaseReadable bool
}

func New() *Client                { path, _ := ResolvePath(); return &Client{path: path} }
func (c *Client) Available() bool { return c.path != "" }

type authStatus struct {
	Identity   string `json:"identity"`
	UserName   string `json:"userName"`
	UserOpenID string `json:"userOpenId"`
	Identities struct {
		User struct {
			UserName string `json:"userName"`
			OpenID   string `json:"openId"`
			Scope    string `json:"scope"`
		} `json:"user"`
	} `json:"identities"`
}

func (c *Client) Status(ctx context.Context) (authStatus, error) {
	var status authStatus
	if !c.Available() {
		return status, fmt.Errorf("lark-cli 未安装")
	}
	raw, err := c.run(ctx, "auth", "status", "--verify")
	if err != nil {
		return status, err
	}
	return decodeAuthStatus(raw)
}

func decodeAuthStatus(raw []byte) (authStatus, error) {
	var status authStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return status, err
	}
	// lark-cli >= 1.0.23 reports identity details under identities.user.
	// Keep the legacy top-level fields as a fallback so older CLI versions
	// remain compatible with the same Syncer contract.
	if status.UserOpenID == "" {
		status.UserOpenID = status.Identities.User.OpenID
	}
	if status.UserName == "" {
		status.UserName = status.Identities.User.UserName
	}
	return status, nil
}
func (c *Client) Connected(ctx context.Context) bool {
	status, err := c.Status(ctx)
	return err == nil && status.Identity == "user"
}
func (c *Client) Self(ctx context.Context) (string, string, error) {
	s, err := c.Status(ctx)
	return s.UserOpenID, s.UserName, err
}

func (c *Client) Chats(ctx context.Context, limit int) ([]model.Chat, error) {
	args := []string{"im", "+chat-list", "--as", "user", "--types", "p2p,group", "--sort", "active_time", "--format", "json"}
	if limit > 0 {
		if limit > 100 {
			limit = 100
		}
		args = append(args, "--page-size", strconv.Itoa(limit))
	} else {
		args = append(args, "--page-all", "--page-limit", "1000")
	}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var root struct {
		Data  json.RawMessage `json:"data"`
		Chats []cliChat       `json:"chats"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	payload := raw
	if len(root.Data) > 0 {
		payload = root.Data
	}
	var body struct {
		Chats []cliChat `json:"chats"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	if len(body.Chats) == 0 {
		body.Chats = root.Chats
	}
	out := make([]model.Chat, 0, len(body.Chats))
	for _, x := range body.Chats {
		typ := x.ChatMode
		if typ == "topic" {
			typ = "group"
		}
		out = append(out, model.Chat{ID: x.ChatID, Name: x.Name, Description: x.Description, Type: typ, Avatar: x.Avatar, External: x.External})
	}
	return out, nil
}

type cliChat struct {
	ChatID      string `json:"chat_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ChatMode    string `json:"chat_mode"`
	Avatar      string `json:"avatar"`
	External    bool   `json:"external"`
}
type cliMessage struct {
	MessageID       string `json:"message_id"`
	ChatID          string `json:"chat_id"`
	MsgType         string `json:"msg_type"`
	CreateTime      any    `json:"create_time"`
	MessagePosition any    `json:"message_position"`
	Content         string `json:"content"`
	Deleted         bool   `json:"deleted"`
	Mentions        []struct {
		ID   string `json:"id"`
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"mentions"`
	Sender struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		SenderName string `json:"sender_name"`
	} `json:"sender"`
}

func (c *Client) MessagesPage(ctx context.Context, chatID, pageToken string) ([]model.Message, string, bool, error) {
	return c.messagesPage(ctx, chatID, pageToken, time.Time{})
}

func (c *Client) MessagesPageSince(ctx context.Context, chatID, pageToken string, since time.Time) ([]model.Message, string, bool, error) {
	return c.messagesPage(ctx, chatID, pageToken, since)
}

// MessagesSince lets lark-cli paginate inside one process. This avoids paying
// Node startup cost for every 50-message page during first initialization.
func (c *Client) MessagesSince(ctx context.Context, chatID string, since time.Time) ([]model.Message, error) {
	args := []string{"im", "+chat-messages-list", "--as", "user", "--chat-id", chatID, "--order", "desc", "--page-size", "50", "--page-all", "--page-limit", "1000", "--no-reactions", "--format", "json"}
	if !since.IsZero() {
		args = append(args, "--start", since.UTC().Format(time.RFC3339))
	}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	messages, _, _, err := decodeMessageList(raw, chatID)
	return messages, err
}

func (c *Client) messagesPage(ctx context.Context, chatID, pageToken string, since time.Time) ([]model.Message, string, bool, error) {
	args := []string{"im", "+chat-messages-list", "--as", "user", "--chat-id", chatID, "--order", "desc", "--page-size", "50", "--no-reactions", "--format", "json"}
	if !since.IsZero() {
		args = append(args, "--start", since.UTC().Format(time.RFC3339))
	}
	if pageToken != "" {
		args = append(args, "--page-token", pageToken)
	}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, "", false, err
	}
	return decodeMessageList(raw, chatID)
}

func decodeMessageList(raw []byte, chatID string) ([]model.Message, string, bool, error) {
	var root struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, "", false, err
	}
	payload := raw
	if len(root.Data) > 0 {
		payload = root.Data
	}
	var body struct {
		Messages  []cliMessage `json:"messages"`
		HasMore   bool         `json:"has_more"`
		PageToken string       `json:"page_token"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, "", false, err
	}
	out := convertMessages(body.Messages, chatID)
	return out, body.PageToken, body.HasMore, nil
}

// MessagesByIDs fetches the canonical metadata for existing messages. The CLI
// limits this endpoint to 50 IDs, so callers should batch larger repairs.
func (c *Client) MessagesByIDs(ctx context.Context, messageIDs []string) ([]model.Message, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if len(messageIDs) > 50 {
		return nil, fmt.Errorf("批量查询消息最多支持 50 个 ID")
	}
	raw, err := c.run(ctx, "im", "+messages-mget", "--as", "user", "--message-ids", strings.Join(messageIDs, ","), "--no-reactions", "--format", "json")
	if err != nil {
		return nil, err
	}
	var root struct {
		Data     json.RawMessage `json:"data"`
		Messages []cliMessage    `json:"messages"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	payload := raw
	if len(root.Data) > 0 {
		payload = root.Data
	}
	var body struct {
		Messages []cliMessage `json:"messages"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return nil, err
	}
	if len(body.Messages) == 0 {
		body.Messages = root.Messages
	}
	return convertMessages(body.Messages, ""), nil
}

func convertMessages(messages []cliMessage, fallbackChatID string) []model.Message {
	out := make([]model.Message, 0, len(messages))
	for _, x := range messages {
		content := x.Content
		if x.Deleted {
			content = "[消息已撤回]"
		}
		name := x.Sender.Name
		if name == "" {
			name = x.Sender.SenderName
		}
		chatID := x.ChatID
		if chatID == "" {
			chatID = fallbackChatID
		}
		mentions := make([]model.Mention, 0, len(x.Mentions))
		for _, mention := range x.Mentions {
			mentions = append(mentions, model.Mention{ID: mention.ID, Key: mention.Key, Name: mention.Name})
		}
		out = append(out, model.Message{ID: x.MessageID, ChatID: chatID, SenderID: x.Sender.ID, SenderName: name, Content: content, Type: x.MsgType, CreatedAt: parseTime(x.CreateTime), Position: parseInteger(x.MessagePosition), Mentions: mentions})
	}
	return out
}

func parseTime(value any) int64 {
	s := fmt.Sprint(value)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n < 10_000_000_000 {
			return n * 1000
		}
		return n
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli()
	}
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

func parseInteger(value any) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
	return n
}
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	return c.runInDir(ctx, "", args...)
}

func (c *Client) runInDir(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.path, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("lark-cli: %s", msg)
	}
	return out, nil
}

// DownloadImage downloads one image resource into dir using a relative output
// name. Keeping the output relative lets lark-cli's path guard confine the
// write to the caller-owned cache directory.
func (c *Client) DownloadImage(ctx context.Context, messageID, imageKey, dir, filename string) error {
	if !c.Available() {
		return fmt.Errorf("lark-cli 未安装")
	}
	raw, err := c.runInDir(ctx, dir, "im", "+messages-resources-download", "--as", "user", "--message-id", messageID, "--file-key", imageKey, "--type", "image", "--output", filename, "--format", "json")
	if err != nil {
		return err
	}
	var result struct {
		OK   bool `json:"ok"`
		Data struct {
			SavedPath string `json:"saved_path"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("解析图片下载结果: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("lark-cli 未确认图片下载成功")
	}
	target := filepath.Join(dir, filename)
	saved := result.Data.SavedPath
	if !filepath.IsAbs(saved) {
		saved = filepath.Join(dir, saved)
	}
	relative, err := filepath.Rel(dir, saved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("lark-cli 返回了缓存目录外的图片路径")
	}
	if saved != target {
		if err := os.Rename(saved, target); err != nil {
			return fmt.Errorf("规范化图片缓存路径: %w", err)
		}
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("图片下载结果不存在: %w", err)
	}
	return nil
}
