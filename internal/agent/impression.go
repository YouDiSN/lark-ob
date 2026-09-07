package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/youdisn/lark-ob/internal/impression"
	appmodel "github.com/youdisn/lark-ob/internal/model"
)

type profileRepository interface {
	InteractionMessagesSince(context.Context, string, int64, int) ([]appmodel.Message, error)
	SelfMessagesSince(context.Context, int64, int) ([]appmodel.Message, error)
	GetPersonImpression(context.Context, string) (impression.PersonImpression, error)
	GetStyleProfile(context.Context, string, string) (impression.StyleProfile, error)
}

type profilePromptMessage struct {
	ID         string `json:"id"`
	ChatID     string `json:"chatId"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content"`
	CreatedAt  int64  `json:"createdAt"`
	IsSelf     bool   `json:"isSelf"`
}

func (e *Engine) GeneratePersonProfile(ctx context.Context, person impression.Person, windowStart, changedSince time.Time) (impression.PersonImpression, *impression.StyleProfile, error) {
	if err := e.requireModel(); err != nil {
		return impression.PersonImpression{}, nil, err
	}
	repo, ok := e.repo.(profileRepository)
	if !ok {
		return impression.PersonImpression{}, nil, fmt.Errorf("画像消息仓库未启用")
	}
	if person.MessageCount < 3 {
		return impression.PersonImpression{}, nil, nil
	}
	messages, err := repo.InteractionMessagesSince(ctx, person.ID, windowStart.UnixMilli(), 5000)
	if err != nil {
		return impression.PersonImpression{}, nil, err
	}
	selected := selectProfileMessages(messages, 240)
	if len(selected) < 3 {
		return impression.PersonImpression{}, nil, nil
	}
	payload, evidence, selfCount, selfLastAt := profilePromptPayload(selected)
	var old *impression.PersonImpression
	if value, oldErr := repo.GetPersonImpression(ctx, person.ID); oldErr == nil {
		old = &value
	}
	data, _ := json.Marshal(struct {
		Person       impression.Person            `json:"person"`
		Previous     *impression.PersonImpression `json:"previous,omitempty"`
		ChangedSince int64                        `json:"changedSince"`
		Messages     []profilePromptMessage       `json:"messages"`
	}{Person: person, Previous: old, ChangedSince: changedSince.UnixMilli(), Messages: payload})
	system := `你是本地个人消息工作台的互动画像生成器。根据最近一个月中目标人物与当前用户的真实消息，更新两类派生资料：
1. impression：对目标人物的“互动印象”，用于帮助当前用户更合适地沟通；
2. relationshipStyle：当前用户与此人的既有表达方式，只描述当前用户怎么说话，不模仿目标人物。
要求：只总结可观察的沟通模式，不做心理诊断，不推断敏感属性，不把单次情绪当作稳定特征。只有跨消息稳定出现的特点才写入标签。已有 previous 仅用于保持稳定，若新证据不支持则应修正。
tags 各 1 到 5 个、每个不超过 8 个汉字；summary 一句话；guidance 必须可以直接指导回复。所有 evidenceMessageIds 必须来自输入。
输出纯 JSON：{"impression":{"tags":["..."],"summary":"...","communicationGuidance":"...","confidence":0.0,"evidenceMessageIds":["..."]},"relationshipStyle":{"tags":["..."],"summary":"...","guidance":"...","confidence":0.0,"evidenceMessageIds":["..."]}}`
	response, err := e.model.Generate(ctx, []*schema.Message{schema.SystemMessage(system), schema.UserMessage(string(data))})
	if err != nil {
		return impression.PersonImpression{}, nil, fmt.Errorf("生成 %s 的基础印象失败: %w", person.Name, err)
	}
	var output struct {
		Impression struct {
			Tags                  []string `json:"tags"`
			Summary               string   `json:"summary"`
			CommunicationGuidance string   `json:"communicationGuidance"`
			Confidence            float64  `json:"confidence"`
			EvidenceMessageIDs    []string `json:"evidenceMessageIds"`
		} `json:"impression"`
		RelationshipStyle struct {
			Tags               []string `json:"tags"`
			Summary            string   `json:"summary"`
			Guidance           string   `json:"guidance"`
			Confidence         float64  `json:"confidence"`
			EvidenceMessageIDs []string `json:"evidenceMessageIds"`
		} `json:"relationshipStyle"`
	}
	if err := decodeJSON(response.Content, &output); err != nil {
		return impression.PersonImpression{}, nil, fmt.Errorf("解析 %s 的基础印象失败: %w", person.Name, err)
	}
	now := time.Now()
	profile := impression.PersonImpression{
		PersonID: person.ID, PersonName: person.Name, Tags: cleanTags(output.Impression.Tags),
		Summary: strings.TrimSpace(output.Impression.Summary), CommunicationGuidance: strings.TrimSpace(output.Impression.CommunicationGuidance),
		Confidence: clampConfidence(output.Impression.Confidence), EvidenceMessageIDs: validEvidence(output.Impression.EvidenceMessageIDs, evidence),
		MessageCount: person.MessageCount, ConversationCount: person.ConversationCount, WindowStart: windowStart.UnixMilli(),
		WindowEnd: now.UnixMilli(), SourceLastMessageAt: person.LastMessageAt, Model: e.config.Model, GeneratedAt: now.UnixMilli(),
	}
	if profile.Summary == "" || len(profile.EvidenceMessageIDs) == 0 {
		return impression.PersonImpression{}, nil, fmt.Errorf("%s 的基础印象缺少摘要或证据", person.Name)
	}
	var relationship *impression.StyleProfile
	if selfCount >= 10 && strings.TrimSpace(output.RelationshipStyle.Summary) != "" {
		relationship = &impression.StyleProfile{
			ScopeType: impression.ScopeRelationship, ScopeID: person.ID, ScopeName: person.Name,
			Tags: cleanTags(output.RelationshipStyle.Tags), Summary: strings.TrimSpace(output.RelationshipStyle.Summary),
			Guidance: strings.TrimSpace(output.RelationshipStyle.Guidance), Confidence: clampConfidence(output.RelationshipStyle.Confidence),
			EvidenceMessageIDs: validEvidence(output.RelationshipStyle.EvidenceMessageIDs, evidence), SampleCount: selfCount,
			WindowStart: windowStart.UnixMilli(), WindowEnd: now.UnixMilli(), SourceLastMessageAt: selfLastAt,
			Model: e.config.Model, GeneratedAt: now.UnixMilli(),
		}
	}
	return profile, relationship, nil
}

func (e *Engine) GenerateSelfStyle(ctx context.Context, windowStart, changedSince time.Time) (impression.StyleProfile, error) {
	if err := e.requireModel(); err != nil {
		return impression.StyleProfile{}, err
	}
	repo, ok := e.repo.(profileRepository)
	if !ok {
		return impression.StyleProfile{}, fmt.Errorf("画像消息仓库未启用")
	}
	messages, err := repo.SelfMessagesSince(ctx, windowStart.UnixMilli(), 5000)
	if err != nil {
		return impression.StyleProfile{}, err
	}
	selected := selectProfileMessages(messages, 240)
	payload, evidence, count, lastAt := profilePromptPayload(selected)
	var old *impression.StyleProfile
	if value, oldErr := repo.GetStyleProfile(ctx, impression.ScopeSelf, "me"); oldErr == nil {
		old = &value
	}
	data, _ := json.Marshal(struct {
		Previous     *impression.StyleProfile `json:"previous,omitempty"`
		ChangedSince int64                    `json:"changedSince"`
		Messages     []profilePromptMessage   `json:"messages"`
	}{Previous: old, ChangedSince: changedSince.UnixMilli(), Messages: payload})
	response, err := e.model.Generate(ctx, []*schema.Message{
		schema.SystemMessage(`你是个人表达风格分析器。输入只包含当前用户本人真实发送的近期消息。总结用户稳定的基础表达方式，而不是消息主题。关注长度、直接程度、正式度、连续短句、标点、表情、称呼与中英文混用。排除代码、日志、卡片模板和转发内容的影响。不要推断人格或敏感属性。tags 1 到 5 个，每个不超过 8 个汉字；guidance 要能直接约束回复生成。证据 ID 必须来自输入。输出纯 JSON：{"tags":["..."],"summary":"...","guidance":"...","confidence":0.0,"evidenceMessageIds":["..."]}`),
		schema.UserMessage(string(data)),
	})
	if err != nil {
		return impression.StyleProfile{}, fmt.Errorf("生成我的基础风格失败: %w", err)
	}
	var output struct {
		Tags               []string `json:"tags"`
		Summary            string   `json:"summary"`
		Guidance           string   `json:"guidance"`
		Confidence         float64  `json:"confidence"`
		EvidenceMessageIDs []string `json:"evidenceMessageIds"`
	}
	if err := decodeJSON(response.Content, &output); err != nil {
		return impression.StyleProfile{}, fmt.Errorf("解析我的基础风格失败: %w", err)
	}
	now := time.Now()
	return impression.StyleProfile{ScopeType: impression.ScopeSelf, ScopeID: "me", ScopeName: "我的基础风格",
		Tags: cleanTags(output.Tags), Summary: strings.TrimSpace(output.Summary), Guidance: strings.TrimSpace(output.Guidance),
		Confidence: clampConfidence(output.Confidence), EvidenceMessageIDs: validEvidence(output.EvidenceMessageIDs, evidence),
		SampleCount: count, WindowStart: windowStart.UnixMilli(), WindowEnd: now.UnixMilli(), SourceLastMessageAt: lastAt,
		Model: e.config.Model, GeneratedAt: now.UnixMilli()}, nil
}

func selectProfileMessages(messages []appmodel.Message, limit int) []appmodel.Message {
	filtered := make([]appmodel.Message, 0, len(messages))
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		lowerType := strings.ToLower(message.Type)
		if content == "" || lowerType == "system" || lowerType == "card" || strings.HasPrefix(content, "<card") || strings.HasPrefix(content, "```json") {
			continue
		}
		filtered = append(filtered, message)
	}
	if len(filtered) <= limit {
		return filtered
	}
	// Preserve both older and recent examples so the model sees stable patterns
	// without sending the entire month on every daily run.
	selected := make([]appmodel.Message, 0, limit)
	step := float64(len(filtered)-1) / float64(limit-1)
	for index := 0; index < limit; index++ {
		selected = append(selected, filtered[int(float64(index)*step)])
	}
	return selected
}

func profilePromptPayload(messages []appmodel.Message) ([]profilePromptMessage, map[string]struct{}, int, int64) {
	payload := make([]profilePromptMessage, 0, len(messages))
	evidence := make(map[string]struct{}, len(messages))
	selfCount, selfLastAt := 0, int64(0)
	for _, message := range messages {
		content := []rune(strings.TrimSpace(message.Content))
		if len(content) > 400 {
			content = content[:400]
		}
		payload = append(payload, profilePromptMessage{ID: message.ID, ChatID: message.ChatID, SenderID: message.SenderID,
			SenderName: message.SenderName, Content: string(content), CreatedAt: message.CreatedAt, IsSelf: message.IsSelf})
		evidence[message.ID] = struct{}{}
		if message.IsSelf {
			selfCount++
			if message.CreatedAt > selfLastAt {
				selfLastAt = message.CreatedAt
			}
		}
	}
	return payload, evidence, selfCount, selfLastAt
}

func validEvidence(ids []string, allowed map[string]struct{}) []string {
	seen := map[string]bool{}
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := allowed[id]; ok && !seen[id] {
			seen[id] = true
			valid = append(valid, id)
		}
	}
	if len(valid) > 20 {
		valid = valid[:20]
	}
	return valid
}

func cleanTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, min(len(tags), 5))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		runes := []rune(tag)
		if len(runes) > 8 {
			tag = string(runes[:8])
		}
		seen[tag] = true
		out = append(out, tag)
		if len(out) == 5 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func clampConfidence(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
