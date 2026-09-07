package model

type Chat struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Type         string `json:"type"`
	Avatar       string `json:"avatar,omitempty"`
	LastMessage  string `json:"lastMessage"`
	LastTime     int64  `json:"lastTime"`
	LastPosition int64  `json:"lastPosition,omitempty"`
	NewMessages  int    `json:"newMessages"`
	MentionCount int    `json:"mentionCount"`
	External     bool   `json:"external"`
	Muted        bool   `json:"muted"`
	InMessageBox bool   `json:"inMessageBox"`
}

type Mention struct {
	ID   string `json:"id"`
	Key  string `json:"key,omitempty"`
	Name string `json:"name,omitempty"`
}

type Message struct {
	ID           string    `json:"id"`
	ChatID       string    `json:"chatId"`
	SenderID     string    `json:"senderId"`
	SenderName   string    `json:"senderName"`
	SenderAvatar string    `json:"senderAvatar,omitempty"`
	Content      string    `json:"content"`
	Type         string    `json:"type"`
	CreatedAt    int64     `json:"createdAt"`
	Position     int64     `json:"position,omitempty"`
	IsSelf       bool      `json:"isSelf"`
	Mentions     []Mention `json:"mentions,omitempty"`
	MentionsSelf bool      `json:"mentionsSelf"`
}

type PersonProfile struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Avatar     string `json:"avatar,omitempty"`
	Base       string `json:"base,omitempty"`
	Department string `json:"department,omitempty"`
	CheckedAt  int64  `json:"checkedAt"`
}

type Status struct {
	Mode       string `json:"mode"`
	Connected  bool   `json:"connected"`
	Configured bool   `json:"configured"`
	LastSync   int64  `json:"lastSync,omitempty"`
	Syncing    bool   `json:"syncing"`
	Error      string `json:"error,omitempty"`
	AuthMode   string `json:"authMode,omitempty"`
}

// AgentChatContext is the durable, per-conversation understanding refreshed
// whenever new messages arrive. It keeps recommendation requests small and
// isolates one conversation's state from every other Agent session.
type AgentChatContext struct {
	ChatID               string   `json:"chatId"`
	Summary              string   `json:"summary"`
	Topics               []string `json:"topics"`
	Participants         []string `json:"participants"`
	ReplyTargetMessageID string   `json:"replyTargetMessageId,omitempty"`
	SourceLastTime       int64    `json:"sourceLastTime"`
	SourceLastPosition   int64    `json:"sourceLastPosition"`
	Model                string   `json:"model"`
	UpdatedAt            int64    `json:"updatedAt"`
}
