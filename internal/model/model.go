package model

type Chat struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
	Avatar      string `json:"avatar,omitempty"`
	LastMessage string `json:"lastMessage"`
	LastTime    int64  `json:"lastTime"`
	Unread      int    `json:"unread"`
	External    bool   `json:"external"`
}

type Message struct {
	ID         string `json:"id"`
	ChatID     string `json:"chatId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	Type       string `json:"type"`
	CreatedAt  int64  `json:"createdAt"`
	IsSelf     bool   `json:"isSelf"`
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
