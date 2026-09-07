package memory

const (
	SubjectPerson = "person"
	SubjectChat   = "chat"
	SourceAgent   = "agent"
	SourceManual  = "manual"

	ImportanceNormal     = "normal"
	ImportanceImportant  = "important"
	ImportanceConstraint = "constraint"
)

// Claim is a durable, evidence-backed memory. Person claims are aggregated
// across conversations while ChatID keeps the conversation provenance.
type Claim struct {
	ID                 int64    `json:"id"`
	SubjectType        string   `json:"subjectType"`
	SubjectID          string   `json:"subjectId"`
	SubjectName        string   `json:"subjectName,omitempty"`
	ChatID             string   `json:"chatId"`
	Category           string   `json:"category"`
	Content            string   `json:"content"`
	Confidence         float64  `json:"confidence"`
	EffectiveScore     float64  `json:"effectiveScore"`
	EvidenceMessageIDs []string `json:"evidenceMessageIds"`
	LastEvidenceAt     int64    `json:"lastEvidenceAt"`
	SourceChatName     string   `json:"sourceChatName,omitempty"`
	SourceChatType     string   `json:"sourceChatType,omitempty"`
	CreatedAt          int64    `json:"createdAt"`
	UpdatedAt          int64    `json:"updatedAt"`
	SourceType         string   `json:"sourceType"`
	Importance         string   `json:"importance"`
	ValidFrom          int64    `json:"validFrom,omitempty"`
	ValidUntil         int64    `json:"validUntil,omitempty"`
	Pinned             bool     `json:"pinned"`
	Status             string   `json:"status"`
}

type ContextRequest struct {
	ChatID    string
	PersonIDs []string
	Query     string
	Limit     int
	ActiveAt  int64
}

type ManualRequest struct {
	SubjectType string `json:"subjectType"`
	SubjectID   string `json:"subjectId"`
	SubjectName string `json:"subjectName"`
	ChatID      string `json:"chatId"`
	Category    string `json:"category"`
	Content     string `json:"content"`
	Importance  string `json:"importance"`
	ValidFrom   int64  `json:"validFrom,omitempty"`
	ValidUntil  int64  `json:"validUntil,omitempty"`
	Pinned      bool   `json:"pinned"`
}

type SubjectOption struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	ChatID   string `json:"chatId"`
	ChatName string `json:"chatName,omitempty"`
	ChatType string `json:"chatType,omitempty"`
}

type SubjectOptions struct {
	People []SubjectOption `json:"people"`
	Chats  []SubjectOption `json:"chats"`
}

type ListRequest struct {
	SubjectType    string
	SourceChatType string
	Query          string
	Limit          int
	Offset         int
}

type Stats struct {
	Total          int `json:"total"`
	PersonMemories int `json:"personMemories"`
	GroupMemories  int `json:"groupMemories"`
	P2PMemories    int `json:"p2pMemories"`
	People         int `json:"people"`
	Groups         int `json:"groups"`
	P2PChats       int `json:"p2pChats"`
}

type Warehouse struct {
	Stats             Stats   `json:"stats"`
	Items             []Claim `json:"items"`
	DecayHalfLifeDays float64 `json:"decayHalfLifeDays"`
	TotalMatches      int     `json:"totalMatches"`
	HasMore           bool    `json:"hasMore"`
	NextOffset        int     `json:"nextOffset"`
}
