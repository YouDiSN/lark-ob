package agent

import "github.com/youdisn/lark-ob/internal/memory"

type Status struct {
	Enabled   bool   `json:"enabled"`
	Framework string `json:"framework"`
	Model     string `json:"model"`
	Endpoint  string `json:"endpoint"`
	Error     string `json:"error,omitempty"`
}

type Suggestion struct {
	Text      string `json:"text"`
	Tone      string `json:"tone,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

type Evidence struct {
	Type      string `json:"type"`
	Reference string `json:"reference"`
	Excerpt   string `json:"excerpt,omitempty"`
}

type ReplyTarget struct {
	MessageID  string `json:"messageId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	CreatedAt  int64  `json:"createdAt"`
	Reason     string `json:"reason"`
}

type Recommendation struct {
	Thinking      string       `json:"thinking"`
	Suggestions   []Suggestion `json:"suggestions"`
	Evidence      []Evidence   `json:"evidence"`
	Model         string       `json:"model"`
	Instruction   string       `json:"instruction,omitempty"`
	AppliedStyles []string     `json:"appliedStyles,omitempty"`
	ReplyTarget   *ReplyTarget `json:"replyTarget,omitempty"`
}

type ChatMemoryResult struct {
	ChatID string         `json:"chatId"`
	Claims []memory.Claim `json:"claims"`
	Model  string         `json:"model"`
}
