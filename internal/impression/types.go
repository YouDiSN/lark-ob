package impression

import "github.com/youdisn/lark-ob/internal/model"

const (
	ScopeSelf         = "self"
	ScopeRelationship = "relationship"
	ScopeChat         = "chat"
)

// Person is a stable participant identity aggregated across conversations.
type Person struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	MessageCount      int    `json:"messageCount"`
	ConversationCount int    `json:"conversationCount"`
	LastMessageAt     int64  `json:"lastMessageAt"`
}

// PersonImpression is a derived, revisable interaction summary. It is kept
// separate from factual memory claims so it can be versioned as one snapshot.
type PersonImpression struct {
	PersonID              string   `json:"personId"`
	PersonName            string   `json:"personName"`
	Tags                  []string `json:"tags"`
	Summary               string   `json:"summary"`
	CommunicationGuidance string   `json:"communicationGuidance"`
	Confidence            float64  `json:"confidence"`
	EvidenceMessageIDs    []string `json:"evidenceMessageIds"`
	MessageCount          int      `json:"messageCount"`
	ConversationCount     int      `json:"conversationCount"`
	WindowStart           int64    `json:"windowStart"`
	WindowEnd             int64    `json:"windowEnd"`
	SourceLastMessageAt   int64    `json:"sourceLastMessageAt"`
	Model                 string   `json:"model"`
	Version               int      `json:"version"`
	GeneratedAt           int64    `json:"generatedAt"`
}

// StyleProfile captures how the local user writes in a specific scope. The
// self profile is the base voice; relationship and chat profiles are modifiers.
type StyleProfile struct {
	ScopeType           string   `json:"scopeType"`
	ScopeID             string   `json:"scopeId"`
	ScopeName           string   `json:"scopeName"`
	Tags                []string `json:"tags"`
	Summary             string   `json:"summary"`
	Guidance            string   `json:"guidance"`
	Confidence          float64  `json:"confidence"`
	EvidenceMessageIDs  []string `json:"evidenceMessageIds"`
	SampleCount         int      `json:"sampleCount"`
	WindowStart         int64    `json:"windowStart"`
	WindowEnd           int64    `json:"windowEnd"`
	SourceLastMessageAt int64    `json:"sourceLastMessageAt"`
	Model               string   `json:"model"`
	Version             int      `json:"version"`
	GeneratedAt         int64    `json:"generatedAt"`
}

type Bundle struct {
	PersonID          string               `json:"personId,omitempty"`
	PersonName        string               `json:"personName,omitempty"`
	DirectoryProfile  *model.PersonProfile `json:"directoryProfile,omitempty"`
	Impression        *PersonImpression    `json:"impression,omitempty"`
	SelfStyle         *StyleProfile        `json:"selfStyle,omitempty"`
	RelationshipStyle *StyleProfile        `json:"relationshipStyle,omitempty"`
}

type UpdateResult struct {
	PeopleFound      int      `json:"peopleFound"`
	ImpressionsSaved int      `json:"impressionsSaved"`
	StylesSaved      int      `json:"stylesSaved"`
	Skipped          int      `json:"skipped"`
	Errors           []string `json:"errors,omitempty"`
}
