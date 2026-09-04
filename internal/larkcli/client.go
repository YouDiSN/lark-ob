package larkcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/youdisn/lark-ob/internal/model"
)

type Client struct{ path string }

func New() *Client                { path, _ := exec.LookPath("lark-cli"); return &Client{path: path} }
func (c *Client) Available() bool { return c.path != "" }

type authStatus struct {
	Identity   string `json:"identity"`
	UserName   string `json:"userName"`
	UserOpenID string `json:"userOpenId"`
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
	err = json.Unmarshal(raw, &status)
	return status, err
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
	MessageID  string `json:"message_id"`
	MsgType    string `json:"msg_type"`
	CreateTime any    `json:"create_time"`
	Content    string `json:"content"`
	Deleted    bool   `json:"deleted"`
	Sender     struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		SenderName string `json:"sender_name"`
	} `json:"sender"`
}

func (c *Client) MessagesPage(ctx context.Context, chatID, pageToken string) ([]model.Message, string, bool, error) {
	args := []string{"im", "+chat-messages-list", "--as", "user", "--chat-id", chatID, "--order", "desc", "--page-size", "50", "--no-reactions", "--format", "json"}
	if pageToken != "" {
		args = append(args, "--page-token", pageToken)
	}
	raw, err := c.run(ctx, args...)
	if err != nil {
		return nil, "", false, err
	}
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
	out := make([]model.Message, 0, len(body.Messages))
	for _, x := range body.Messages {
		content := x.Content
		if x.Deleted {
			content = "[消息已撤回]"
		}
		name := x.Sender.Name
		if name == "" {
			name = x.Sender.SenderName
		}
		out = append(out, model.Message{ID: x.MessageID, ChatID: chatID, SenderID: x.Sender.ID, SenderName: name, Content: content, Type: x.MsgType, CreatedAt: parseTime(x.CreateTime)})
	}
	return out, body.PageToken, body.HasMore, nil
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
	for _, layout := range []string{"2006-01-02 15:04:05 -0700 MST", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}
func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.path, args...)
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
