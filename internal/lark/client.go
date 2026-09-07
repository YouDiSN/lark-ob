package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/youdisn/lark-ob/internal/model"
)

type Config struct{ AppID, AppSecret, APIBase, AuthBase, RedirectURL string }
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}
type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client       { return &Client{cfg: cfg, http: &http.Client{Timeout: 20 * time.Second}} }
func (c *Client) Configured() bool { return c.cfg.AppID != "" && c.cfg.AppSecret != "" }

func (c *Client) UserInfo(ctx context.Context, token string) (string, string, error) {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OpenID string `json:"open_id"`
			Name   string `json:"name"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/open-apis/authen/v1/user_info", token, nil, &out); err != nil {
		return "", "", err
	}
	if out.Code != 0 {
		return "", "", fmt.Errorf("user info: %s (%d)", out.Msg, out.Code)
	}
	return out.Data.OpenID, out.Data.Name, nil
}

func (c *Client) AuthorizeURL(state string) string {
	q := url.Values{"app_id": {c.cfg.AppID}, "redirect_uri": {c.cfg.RedirectURL}, "state": {state}}
	return strings.TrimRight(c.cfg.AuthBase, "/") + "/open-apis/authen/v1/authorize?" + q.Encode()
}

func (c *Client) ExchangeCode(ctx context.Context, code string) (Token, error) {
	body := map[string]string{"grant_type": "authorization_code", "client_id": c.cfg.AppID, "client_secret": c.cfg.AppSecret, "code": code, "redirect_uri": c.cfg.RedirectURL}
	return c.tokenRequest(ctx, body)
}

func (c *Client) Refresh(ctx context.Context, refresh string) (Token, error) {
	body := map[string]string{"grant_type": "refresh_token", "client_id": c.cfg.AppID, "client_secret": c.cfg.AppSecret, "refresh_token": refresh}
	return c.tokenRequest(ctx, body)
}

func (c *Client) tokenRequest(ctx context.Context, body any) (Token, error) {
	var out struct {
		Code         int    `json:"code"`
		Msg          string `json:"msg"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Data         *struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int64  `json:"expires_in"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/open-apis/authen/v2/oauth/token", "", body, &out); err != nil {
		return Token{}, err
	}
	if out.Code != 0 {
		return Token{}, fmt.Errorf("lark oauth: %s (%d)", out.Msg, out.Code)
	}
	if out.Data != nil {
		out.AccessToken = out.Data.AccessToken
		out.RefreshToken = out.Data.RefreshToken
		out.ExpiresIn = out.Data.ExpiresIn
	}
	if out.AccessToken == "" {
		return Token{}, fmt.Errorf("lark oauth returned no access token")
	}
	return Token{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, ExpiresAt: time.Now().Add(time.Duration(out.ExpiresIn) * time.Second).Unix()}, nil
}

type chatPage struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Items []struct {
			ChatID      string `json:"chat_id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			ChatMode    string `json:"chat_mode"`
			Avatar      string `json:"avatar"`
			External    bool   `json:"external"`
		} `json:"items"`
		HasMore   bool   `json:"has_more"`
		PageToken string `json:"page_token"`
	} `json:"data"`
}

func (c *Client) Chats(ctx context.Context, token string, limit int) ([]model.Chat, error) {
	var out []model.Chat
	pageToken := ""
	for {
		q := url.Values{"user_id_type": {"open_id"}, "sort_type": {"ByActiveTimeDesc"}, "types": {"p2p,group"}, "page_size": {"100"}}
		if pageToken != "" {
			q.Set("page_token", pageToken)
		}
		var page chatPage
		if err := c.do(ctx, http.MethodGet, "/open-apis/im/v1/chats?"+q.Encode(), token, nil, &page); err != nil {
			return nil, err
		}
		if page.Code != 0 {
			return nil, fmt.Errorf("list chats: %s (%d)", page.Msg, page.Code)
		}
		for _, x := range page.Data.Items {
			typ := x.ChatMode
			if typ == "" {
				typ = "group"
			}
			out = append(out, model.Chat{ID: x.ChatID, Name: x.Name, Description: x.Description, Type: typ, Avatar: x.Avatar, External: x.External})
		}
		if !page.Data.HasMore || page.Data.PageToken == "" || (limit > 0 && len(out) >= limit) {
			break
		}
		pageToken = page.Data.PageToken
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (c *Client) Messages(ctx context.Context, token, chatID string) ([]model.Message, error) {
	messages, _, _, err := c.MessagesPage(ctx, token, chatID, "")
	return messages, err
}

func (c *Client) MessagesPage(ctx context.Context, token, chatID, pageToken string) ([]model.Message, string, bool, error) {
	return c.messagesPage(ctx, token, chatID, pageToken, time.Time{})
}

func (c *Client) MessagesPageSince(ctx context.Context, token, chatID, pageToken string, since time.Time) ([]model.Message, string, bool, error) {
	return c.messagesPage(ctx, token, chatID, pageToken, since)
}

func (c *Client) messagesPage(ctx context.Context, token, chatID, pageToken string, since time.Time) ([]model.Message, string, bool, error) {
	q := url.Values{"container_id_type": {"chat"}, "container_id": {chatID}, "sort_type": {"ByCreateTimeDesc"}, "page_size": {"50"}, "with_sender_name": {"true"}, "only_thread_root_messages": {"true"}}
	if !since.IsZero() {
		q.Set("start_time", strconv.FormatInt(since.Unix(), 10))
	}
	if pageToken != "" {
		q.Set("page_token", pageToken)
	}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			HasMore   bool   `json:"has_more"`
			PageToken string `json:"page_token"`
			Items     []struct {
				MessageID string `json:"message_id"`
				MsgType   string `json:"msg_type"`
				Body      struct {
					Content string `json:"content"`
				} `json:"body"`
				CreateTime string `json:"create_time"`
				Sender     struct {
					ID         string `json:"id"`
					SenderType string `json:"sender_type"`
					Name       string `json:"sender_name"`
				} `json:"sender"`
				Mentions []struct {
					ID   string `json:"id"`
					Key  string `json:"key"`
					Name string `json:"name"`
				} `json:"mentions"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/open-apis/im/v1/messages?"+q.Encode(), token, nil, &out); err != nil {
		return nil, "", false, err
	}
	if out.Code != 0 {
		return nil, "", false, fmt.Errorf("list messages: %s (%d)", out.Msg, out.Code)
	}
	result := make([]model.Message, 0, len(out.Data.Items))
	for _, x := range out.Data.Items {
		ts, _ := strconv.ParseInt(x.CreateTime, 10, 64)
		content := decodeContent(x.MsgType, x.Body.Content)
		mentions := make([]model.Mention, 0, len(x.Mentions))
		for _, mention := range x.Mentions {
			mentions = append(mentions, model.Mention{ID: mention.ID, Key: mention.Key, Name: mention.Name})
		}
		result = append(result, model.Message{ID: x.MessageID, ChatID: chatID, SenderID: x.Sender.ID, SenderName: x.Sender.Name, Content: content, Type: x.MsgType, CreatedAt: ts, Mentions: mentions})
	}
	return result, out.Data.PageToken, out.Data.HasMore, nil
}

func decodeContent(kind, raw string) string {
	if kind == "text" {
		var v struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(raw), &v) == nil && v.Text != "" {
			return v.Text
		}
	}
	if kind == "image" {
		return "[图片]"
	}
	if kind == "file" {
		return "[文件]"
	}
	if kind == "audio" {
		return "[语音]"
	}
	if kind == "media" {
		return "[视频]"
	}
	if kind == "sticker" {
		return "[表情]"
	}
	if kind == "post" || kind == "interactive" {
		var v any
		if json.Unmarshal([]byte(raw), &v) == nil {
			var parts []string
			collectText(v, &parts)
			if len(parts) > 0 {
				return strings.Join(parts, " ")
			}
		}
	}
	if raw == "" {
		return "[" + kind + "]"
	}
	return raw
}

func collectText(v any, parts *[]string) {
	switch x := v.(type) {
	case []any:
		for _, item := range x {
			collectText(item, parts)
		}
	case map[string]any:
		for _, key := range []string{"title", "text", "content"} {
			if value, exists := x[key]; exists {
				if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
					*parts = append(*parts, strings.TrimSpace(text))
				} else {
					collectText(value, parts)
				}
			}
		}
		for key, value := range x {
			if key != "title" && key != "text" && key != "content" {
				collectText(value, parts)
			}
		}
	}
}

func (c *Client) do(ctx context.Context, method, path, token string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.APIBase, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("lark http %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
