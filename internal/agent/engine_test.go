package agent

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/youdisn/lark-ob/internal/knowledge"
	"github.com/youdisn/lark-ob/internal/memory"
	appmodel "github.com/youdisn/lark-ob/internal/model"
)

type fakeChatModel struct {
	content  string
	contents []string
	calls    int
	messages []*schema.Message
}

func (f *fakeChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	f.calls++
	f.messages = messages
	content := f.content
	if len(f.contents) > 0 {
		index := f.calls - 1
		if index >= len(f.contents) {
			index = len(f.contents) - 1
		}
		content = f.contents[index]
	}
	return schema.AssistantMessage(content, nil), nil
}

func TestExtractChatMemoryRetriesMalformedLongResponseWithReducedAttempt(t *testing.T) {
	repo := &fakeRepository{
		chat:     appmodel.Chat{ID: "chat-1", Name: "大群", Type: "group"},
		messages: []appmodel.Message{{ID: "m1", ChatID: "chat-1", SenderID: "u1", SenderName: "用户 A", Content: "周五交付", CreatedAt: 1}},
	}
	chatModel := &fakeChatModel{contents: []string{"not-json", `{"claims":[{"subjectType":"chat","subjectId":"chat-1","category":"project_context","content":"周五交付","confidence":0.9,"evidenceMessageIds":["m1"]}]}`}}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(&fakeMemoryRepository{}))

	result, err := engine.ExtractChatMemory(context.Background(), "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if chatModel.calls != 2 || len(result.Claims) != 2 {
		t.Fatalf("fallback calls=%d claims=%#v", chatModel.calls, result.Claims)
	}
}

func TestExtractChatMemoryClearsMessageBoxWithoutModelCall(t *testing.T) {
	repo := &fakeRepository{chat: appmodel.Chat{ID: "box", Name: "消息盒子群", Type: "group", InMessageBox: true}}
	memoryRepo := &fakeMemoryRepository{claims: []memory.Claim{{ChatID: "box", SubjectType: memory.SubjectChat, SubjectID: "box", Content: "旧记忆"}}}
	chatModel := &fakeChatModel{content: `{"claims":[]}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(memoryRepo))

	result, err := engine.ExtractChatMemory(context.Background(), "box")
	if err != nil {
		t.Fatal(err)
	}
	if chatModel.calls != 0 || len(result.Claims) != 0 || len(memoryRepo.claims) != 0 {
		t.Fatalf("model calls=%d result=%#v stored=%#v", chatModel.calls, result.Claims, memoryRepo.claims)
	}
}
func (f *fakeChatModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	f.calls++
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage(f.content, nil)}), nil
}
func (f *fakeChatModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return f, nil
}

type fakeRepository struct {
	chat     appmodel.Chat
	messages []appmodel.Message
	source   knowledge.Source
	chunks   []knowledge.Chunk
	summary  knowledge.Summary
}

func (f *fakeRepository) Chat(context.Context, string) (appmodel.Chat, error) { return f.chat, nil }
func (f *fakeRepository) Messages(context.Context, string, int) ([]appmodel.Message, error) {
	return f.messages, nil
}
func (f *fakeRepository) MessagesSince(context.Context, string, int64, int) ([]appmodel.Message, error) {
	return f.messages, nil
}
func (f *fakeRepository) GetKnowledgeSource(context.Context, int64) (knowledge.Source, error) {
	return f.source, nil
}
func (f *fakeRepository) KnowledgeChunks(context.Context, int64) ([]knowledge.Chunk, error) {
	return f.chunks, nil
}
func (f *fakeRepository) SearchKnowledge(context.Context, string, knowledge.SearchOptions) ([]knowledge.SearchResult, error) {
	return nil, nil
}
func (f *fakeRepository) SaveKnowledgeSummary(_ context.Context, summary knowledge.Summary) (knowledge.Summary, error) {
	f.summary = summary
	return summary, nil
}
func (f *fakeRepository) GetKnowledgeSummary(context.Context, int64) (knowledge.Summary, error) {
	return knowledge.Summary{}, nil
}

type fakeMemoryRepository struct{ claims []memory.Claim }

type fakeContextRepository struct {
	*fakeRepository
	context appmodel.AgentChatContext
}

type fakeDirectoryRepository struct {
	*fakeRepository
	profiles map[string]appmodel.PersonProfile
}

func (f *fakeDirectoryRepository) PersonProfile(_ context.Context, id string) (appmodel.PersonProfile, error) {
	profile, ok := f.profiles[id]
	if !ok {
		return appmodel.PersonProfile{}, sql.ErrNoRows
	}
	return profile, nil
}

func (f *fakeContextRepository) GetAgentChatContext(_ context.Context, _ string) (appmodel.AgentChatContext, error) {
	if f.context.ChatID == "" {
		return appmodel.AgentChatContext{}, sql.ErrNoRows
	}
	return f.context, nil
}

func (f *fakeContextRepository) SaveAgentChatContext(_ context.Context, value appmodel.AgentChatContext) error {
	f.context = value
	return nil
}

func (f *fakeMemoryRepository) ReplaceChatMemoryClaims(_ context.Context, _ string, claims []memory.Claim) ([]memory.Claim, error) {
	f.claims = claims
	return claims, nil
}
func (f *fakeMemoryRepository) ListChatMemoryClaims(context.Context, string, int) ([]memory.Claim, error) {
	return f.claims, nil
}
func (f *fakeMemoryRepository) SearchMemoryContext(context.Context, memory.ContextRequest) ([]memory.Claim, error) {
	return f.claims, nil
}
func (f *fakeMemoryRepository) ListMemoryClaims(context.Context, memory.ListRequest) ([]memory.Claim, error) {
	return f.claims, nil
}
func (f *fakeMemoryRepository) MemoryStats(context.Context) (memory.Stats, error) {
	return memory.Stats{Total: len(f.claims)}, nil
}
func (f *fakeMemoryRepository) DeleteMemoryClaim(context.Context, int64) error { return nil }
func (f *fakeMemoryRepository) SaveManualMemoryClaim(_ context.Context, claim memory.Claim) (memory.Claim, error) {
	f.claims = append(f.claims, claim)
	return claim, nil
}
func (f *fakeMemoryRepository) UpdateManualMemoryClaim(_ context.Context, _ int64, claim memory.Claim) (memory.Claim, error) {
	return claim, nil
}
func (f *fakeMemoryRepository) MemorySubjectOptions(context.Context) (memory.SubjectOptions, error) {
	return memory.SubjectOptions{}, nil
}

func TestExtractChatMemoryKeepsStableSubjectsAndEvidence(t *testing.T) {
	repo := &fakeRepository{
		chat: appmodel.Chat{ID: "chat-1", Name: "项目群", Type: "group"},
		messages: []appmodel.Message{
			{ID: "m2", ChatID: "chat-1", SenderID: "u1", SenderName: "用户 A", Content: "周五交付", CreatedAt: 2},
			{ID: "m1", ChatID: "chat-1", SenderID: "u2", SenderName: "用户 B", Content: "喜欢简短回复", CreatedAt: 1},
		},
	}
	memoryRepo := &fakeMemoryRepository{}
	chatModel := &fakeChatModel{content: `{"claims":[
		{"subjectType":"person","subjectId":"u1","category":"commitment","content":"周五交付","confidence":0.9,"evidenceMessageIds":["m2"]},
		{"subjectType":"chat","subjectId":"wrong","category":"project_context","content":"项目处于交付阶段","confidence":0.7,"evidenceMessageIds":["m1"]},
		{"subjectType":"person","subjectId":"unknown","category":"fact","content":"无效人物","confidence":1,"evidenceMessageIds":["m2"]},
		{"subjectType":"person","subjectId":"u2","category":"preference","content":"无效证据","confidence":1,"evidenceMessageIds":["missing"]}
	]}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(memoryRepo))

	result, err := engine.ExtractChatMemory(context.Background(), "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 3 {
		t.Fatalf("got %d claims, want 3: %#v", len(result.Claims), result.Claims)
	}
	if result.Claims[0].SubjectName != "用户 A" {
		t.Fatalf("person name must come from message identity: %#v", result.Claims[0])
	}
	if result.Claims[1].SubjectID != "chat-1" || result.Claims[1].SubjectName != "项目群" {
		t.Fatalf("chat subject must be normalized: %#v", result.Claims[1])
	}
	if result.Claims[2].SubjectID != "u2" || len(result.Claims[2].EvidenceMessageIDs) != 1 {
		t.Fatalf("participant without a durable model claim needs an evidence-backed fallback: %#v", result.Claims[2])
	}
}

func TestStatusDoesNotExposeAPIKey(t *testing.T) {
	engine := New(context.Background(), Config{APIKey: "secret-only"}, &fakeRepository{}, memory.NewService(&fakeMemoryRepository{}))
	status := engine.Status()
	if status.Enabled || status.Error == "" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestSummarizeKnowledgeUsesImportedChunks(t *testing.T) {
	repo := &fakeRepository{
		source: knowledge.Source{ID: 7, Title: "产品手册", URL: "https://example.test/doc"},
		chunks: []knowledge.Chunk{{ID: 11, Heading: "交付", BlockID: "block-1", Content: "周五交付第一版"}},
	}
	chatModel := &fakeChatModel{content: `{"summary":"第一版周五交付","facts":["交付时间为周五"]}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(&fakeMemoryRepository{}))

	summary, err := engine.SummarizeKnowledge(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SourceID != 7 || summary.Model != "grok-4.5" || len(summary.Facts) != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if repo.summary.Summary == "" {
		t.Fatal("summary was not persisted")
	}
}

func TestRecommendReplyUsesOneModelRoundTrip(t *testing.T) {
	repo := &fakeRepository{
		chat:     appmodel.Chat{ID: "chat-1", Name: "项目群", Type: "group"},
		messages: []appmodel.Message{{ID: "m1", ChatID: "chat-1", SenderID: "u1", SenderName: "用户 A", Content: "这个问题怎么处理？", CreatedAt: 1}},
	}
	memoryRepo := &fakeMemoryRepository{claims: []memory.Claim{{SubjectType: memory.SubjectPerson, SubjectID: "u1", Content: "偏好简洁回复"}}}
	chatModel := &fakeChatModel{content: `{"thinking":"直接回答","suggestions":[{"text":"我来处理。"}],"evidence":[]}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(memoryRepo))

	result, err := engine.RecommendReply(context.Background(), "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 1 || result.Suggestions[0].Text != "我来处理。" {
		t.Fatalf("unexpected recommendation: %#v", result)
	}
	if chatModel.calls != 1 {
		t.Fatalf("recommendation used %d model calls, want 1", chatModel.calls)
	}
}

func TestRecommendReplyIncludesDirectoryFactsAndPhysicalConstraint(t *testing.T) {
	repo := &fakeDirectoryRepository{fakeRepository: &fakeRepository{
		chat: appmodel.Chat{ID: "chat-1", Name: "费阳", Type: "p2p"},
		messages: []appmodel.Message{
			{ID: "self", ChatID: "chat-1", SenderID: "me", SenderName: "我", Content: "看起来不错", CreatedAt: 1, IsSelf: true},
			{ID: "target", ChatID: "chat-1", SenderID: "other", SenderName: "费阳", Content: "准备做红豆汤", CreatedAt: 2},
		},
	}, profiles: map[string]appmodel.PersonProfile{
		"me":    {ID: "me", Base: "上海", Department: "技术"},
		"other": {ID: "other", Base: "重庆", Department: "设计"},
	}}
	chatModel := &fakeChatModel{content: `{"suggestions":[{"text":"做好了发我看看"}]}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(&fakeMemoryRepository{}))
	if _, err := engine.RecommendReply(context.Background(), "chat-1"); err != nil {
		t.Fatal(err)
	}
	var prompt string
	for _, message := range chatModel.messages {
		prompt += message.Content
	}
	if !strings.Contains(prompt, `"base":"上海"`) || !strings.Contains(prompt, `"base":"重庆"`) || !strings.Contains(prompt, "不要建议依赖现实接触") {
		t.Fatalf("directory facts or physical constraint missing: %s", prompt)
	}
}

func TestRecommendReplyAppliesUserInstructionToPreviousCandidates(t *testing.T) {
	repo := &fakeRepository{
		chat:     appmodel.Chat{ID: "chat-1", Name: "项目群", Type: "group"},
		messages: []appmodel.Message{{ID: "m1", ChatID: "chat-1", SenderID: "u1", SenderName: "用户 A", Content: "今天能交付吗？", CreatedAt: 1}},
	}
	chatModel := &fakeChatModel{content: `{"thinking":"调整为正式语气","suggestions":[{"text":"可以，今天将按计划交付。"},{"text":"确认今天完成交付。"},{"text":"今日可以交付，我会同步最终结果。"}],"evidence":[]}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(&fakeMemoryRepository{}))

	result, err := engine.RecommendReplyWithInstruction(context.Background(), "chat-1", "", "给我一个更严肃的答复", []Suggestion{{Text: "可以的"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Suggestions) != 3 || result.Instruction != "给我一个更严肃的答复" {
		t.Fatalf("unexpected refined recommendation: %#v", result)
	}
	var prompt string
	for _, message := range chatModel.messages {
		prompt += message.Content
	}
	if !strings.Contains(prompt, "给我一个更严肃的答复") || !strings.Contains(prompt, "可以的") {
		t.Fatalf("refinement context missing from prompt: %s", prompt)
	}
}

func TestUpdateChatContextPersistsAndSkipsUnchangedChat(t *testing.T) {
	ctx := context.Background()
	repo := &fakeContextRepository{fakeRepository: &fakeRepository{
		chat:     appmodel.Chat{ID: "chat-1", Name: "项目群", Type: "group", LastTime: 100, LastPosition: 7},
		messages: []appmodel.Message{{ID: "m1", ChatID: "chat-1", SenderID: "u1", SenderName: "用户 A", Content: "周五能交付吗？", CreatedAt: 100, Position: 7}},
	}}
	chatModel := &fakeChatModel{content: `{"summary":"正在确认周五交付时间","topics":["交付时间"],"replyTargetMessageId":"m1"}`}
	engine := NewWithModel(Config{Model: "grok-4.5"}, chatModel, repo, memory.NewService(&fakeMemoryRepository{}))
	value, err := engine.UpdateChatContext(ctx, "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	if value.Summary != "用户 A：周五能交付吗？" || value.ReplyTargetMessageID != "m1" || value.SourceLastPosition != 7 {
		t.Fatalf("unexpected context: %#v", value)
	}
	persisted, err := repo.GetAgentChatContext(ctx, "chat-1")
	if err != nil || persisted.Summary != value.Summary {
		t.Fatalf("context was not persisted: %#v, %v", persisted, err)
	}
	if _, err := engine.UpdateChatContext(ctx, "chat-1"); err != nil {
		t.Fatal(err)
	}
	if chatModel.calls != 0 {
		t.Fatalf("local context processing unexpectedly used %d model calls", chatModel.calls)
	}
}

func TestReplyTargetPrefersUnansweredMentionOverLaterGroupChatter(t *testing.T) {
	messages := []appmodel.Message{
		{ID: "mention", SenderID: "u1", SenderName: "用户 A", Content: "@我 这个问题怎么处理？", CreatedAt: 100, MentionsSelf: true},
		{ID: "later", SenderID: "u2", SenderName: "用户 B", Content: "我补充一句别的内容", CreatedAt: 200},
	}
	target, err := selectReplyTarget(messages, "")
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.MessageID != "mention" || target.Reason != "mention" {
		t.Fatalf("later chatter replaced mention target: %#v", target)
	}
	selected, err := selectReplyTarget(messages, "later")
	if err != nil {
		t.Fatal(err)
	}
	if selected == nil || selected.MessageID != "later" || selected.Reason != "selected" {
		t.Fatalf("explicit target was not honored: %#v", selected)
	}
}

func TestReplyTargetIgnoresMentionAlreadyFollowedBySelfReply(t *testing.T) {
	messages := []appmodel.Message{
		{ID: "mention", SenderID: "u1", Content: "@我 请确认", CreatedAt: 100, MentionsSelf: true},
		{ID: "self", SenderID: "self", Content: "已经确认", CreatedAt: 200, IsSelf: true},
		{ID: "later", SenderID: "u2", Content: "新的讨论", CreatedAt: 300},
	}
	target, err := selectReplyTarget(messages, "")
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.MessageID != "later" || target.Reason != "latest" {
		t.Fatalf("answered mention should not remain pending: %#v", target)
	}
}
