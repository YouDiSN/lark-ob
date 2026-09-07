package memory

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type Repository interface {
	ReplaceChatMemoryClaims(context.Context, string, []Claim) ([]Claim, error)
	ListChatMemoryClaims(context.Context, string, int) ([]Claim, error)
	SearchMemoryContext(context.Context, ContextRequest) ([]Claim, error)
	ListMemoryClaims(context.Context, ListRequest) ([]Claim, error)
	MemoryStats(context.Context) (Stats, error)
	DeleteMemoryClaim(context.Context, int64) error
	SaveManualMemoryClaim(context.Context, Claim) (Claim, error)
	UpdateManualMemoryClaim(context.Context, int64, Claim) (Claim, error)
	MemorySubjectOptions(context.Context) (SubjectOptions, error)
}

type Config struct {
	DecayHalfLifeDays float64
	Now               func() time.Time
}

type Service struct {
	repository        Repository
	decayHalfLifeDays float64
	now               func() time.Time
}

func NewService(repository Repository) *Service { return NewServiceWithConfig(repository, Config{}) }

func NewServiceWithConfig(repository Repository, config Config) *Service {
	if config.DecayHalfLifeDays <= 0 {
		config.DecayHalfLifeDays = 90
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{repository: repository, decayHalfLifeDays: config.DecayHalfLifeDays, now: config.Now}
}

func (s *Service) ReplaceChat(ctx context.Context, chatID string, claims []Claim) ([]Claim, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, fmt.Errorf("会话 ID 不能为空")
	}
	clean := make([]Claim, 0, len(claims))
	for _, claim := range claims {
		claim.ChatID = chatID
		claim.SubjectType = strings.TrimSpace(claim.SubjectType)
		claim.SubjectID = strings.TrimSpace(claim.SubjectID)
		claim.SubjectName = strings.TrimSpace(claim.SubjectName)
		claim.Category = strings.TrimSpace(claim.Category)
		claim.Content = strings.TrimSpace(claim.Content)
		if claim.Content == "" || claim.SubjectID == "" {
			continue
		}
		if claim.SubjectType != SubjectPerson && claim.SubjectType != SubjectChat {
			continue
		}
		if claim.Confidence < 0 {
			claim.Confidence = 0
		}
		if claim.Confidence > 1 {
			claim.Confidence = 1
		}
		clean = append(clean, claim)
	}
	claims, err := s.repository.ReplaceChatMemoryClaims(ctx, chatID, clean)
	return s.scoreAndSort(claims, len(claims)), err
}

func (s *Service) ListChat(ctx context.Context, chatID string) ([]Claim, error) {
	claims, err := s.repository.ListChatMemoryClaims(ctx, chatID, 100)
	if err != nil {
		return nil, err
	}
	claims = s.withStatus(claims)
	active := claims[:0]
	for _, claim := range claims {
		if claim.Status == "active" {
			active = append(active, claim)
		}
	}
	return s.scoreAndSort(active, 100), nil
}

func (s *Service) Context(ctx context.Context, request ContextRequest) ([]Claim, error) {
	if request.Limit <= 0 || request.Limit > 100 {
		request.Limit = 40
	}
	requestedLimit := request.Limit
	request.Limit = min(request.Limit*4, 400)
	request.ActiveAt = s.now().UnixMilli()
	claims, err := s.repository.SearchMemoryContext(ctx, request)
	return s.scoreAndSort(claims, requestedLimit), err
}

func (s *Service) Warehouse(ctx context.Context, request ListRequest) (Warehouse, error) {
	request.SubjectType = strings.TrimSpace(request.SubjectType)
	if request.SubjectType != "" && request.SubjectType != SubjectPerson && request.SubjectType != SubjectChat {
		return Warehouse{}, fmt.Errorf("无效的记忆类型")
	}
	request.SourceChatType = strings.TrimSpace(request.SourceChatType)
	if request.SourceChatType != "" && request.SourceChatType != "p2p" && request.SourceChatType != "group" {
		return Warehouse{}, fmt.Errorf("无效的来源会话类型")
	}
	if request.SourceChatType != "" && request.SubjectType != SubjectChat {
		return Warehouse{}, fmt.Errorf("来源会话类型仅适用于会话记忆")
	}
	if request.Limit <= 0 || request.Limit > 500 {
		request.Limit = 100
	}
	if request.Offset < 0 {
		request.Offset = 0
	}
	pageLimit, pageOffset := request.Limit, request.Offset
	request.Limit = 5000
	request.Offset = 0
	claims, err := s.repository.ListMemoryClaims(ctx, request)
	if err != nil {
		return Warehouse{}, err
	}
	stats, err := s.repository.MemoryStats(ctx)
	if err != nil {
		return Warehouse{}, err
	}
	claims = s.withStatus(claims)
	claims = s.scoreAndSort(claims, len(claims))
	totalMatches := len(claims)
	if pageOffset > totalMatches {
		pageOffset = totalMatches
	}
	end := min(pageOffset+pageLimit, totalMatches)
	return Warehouse{
		Stats: stats, Items: claims[pageOffset:end], DecayHalfLifeDays: s.decayHalfLifeDays,
		TotalMatches: totalMatches, HasMore: end < totalMatches, NextOffset: end,
	}, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return fmt.Errorf("无效的记忆 ID")
	}
	return s.repository.DeleteMemoryClaim(ctx, id)
}

func (s *Service) SubjectOptions(ctx context.Context) (SubjectOptions, error) {
	options, err := s.repository.MemorySubjectOptions(ctx)
	if options.People == nil {
		options.People = []SubjectOption{}
	}
	if options.Chats == nil {
		options.Chats = []SubjectOption{}
	}
	return options, err
}

func (s *Service) CreateManual(ctx context.Context, request ManualRequest) (Claim, error) {
	claim, err := s.validateManual(ctx, request)
	if err != nil {
		return Claim{}, err
	}
	claim, err = s.repository.SaveManualMemoryClaim(ctx, claim)
	if err != nil {
		return Claim{}, err
	}
	return s.decorate(claim), nil
}

func (s *Service) UpdateManual(ctx context.Context, id int64, request ManualRequest) (Claim, error) {
	if id <= 0 {
		return Claim{}, fmt.Errorf("无效的记忆 ID")
	}
	claim, err := s.validateManual(ctx, request)
	if err != nil {
		return Claim{}, err
	}
	claim, err = s.repository.UpdateManualMemoryClaim(ctx, id, claim)
	if err != nil {
		return Claim{}, err
	}
	return s.decorate(claim), nil
}

func (s *Service) validateManual(ctx context.Context, request ManualRequest) (Claim, error) {
	request.SubjectType = strings.TrimSpace(request.SubjectType)
	request.SubjectID = strings.TrimSpace(request.SubjectID)
	request.ChatID = strings.TrimSpace(request.ChatID)
	request.Content = strings.TrimSpace(request.Content)
	request.Category = strings.TrimSpace(request.Category)
	request.Importance = strings.TrimSpace(request.Importance)
	if request.SubjectType != SubjectPerson && request.SubjectType != SubjectChat {
		return Claim{}, fmt.Errorf("请选择人物或会话")
	}
	if request.SubjectID == "" || request.ChatID == "" {
		return Claim{}, fmt.Errorf("请选择记忆对象")
	}
	if request.Content == "" {
		return Claim{}, fmt.Errorf("记忆内容不能为空")
	}
	if len([]rune(request.Content)) > 500 {
		return Claim{}, fmt.Errorf("记忆内容不能超过 500 个字符")
	}
	allowedCategories := map[string]bool{"fact": true, "preference": true, "relationship": true, "commitment": true, "temporary": true, "constraint": true, "project_context": true}
	if !allowedCategories[request.Category] {
		return Claim{}, fmt.Errorf("无效的记忆分类")
	}
	if request.Importance != ImportanceNormal && request.Importance != ImportanceImportant && request.Importance != ImportanceConstraint {
		return Claim{}, fmt.Errorf("无效的重要级别")
	}
	if request.ValidFrom < 0 || request.ValidUntil < 0 || (request.ValidUntil > 0 && request.ValidFrom > 0 && request.ValidUntil <= request.ValidFrom) {
		return Claim{}, fmt.Errorf("失效时间必须晚于生效时间")
	}
	options, err := s.repository.MemorySubjectOptions(ctx)
	if err != nil {
		return Claim{}, err
	}
	var selected *SubjectOption
	if request.SubjectType == SubjectPerson {
		for index := range options.People {
			if options.People[index].ID == request.SubjectID {
				selected = &options.People[index]
				break
			}
		}
	} else {
		for index := range options.Chats {
			if options.Chats[index].ID == request.SubjectID {
				selected = &options.Chats[index]
				break
			}
		}
	}
	if selected == nil {
		return Claim{}, fmt.Errorf("记忆对象不存在或位于消息盒子")
	}
	if request.SubjectType == SubjectChat {
		request.ChatID = selected.ChatID
	} else if request.ChatID != selected.ChatID {
		// Person memories are global; the most recent visible conversation is
		// retained only as provenance and is not part of their retrieval scope.
		request.ChatID = selected.ChatID
	}
	return Claim{SubjectType: request.SubjectType, SubjectID: request.SubjectID, SubjectName: selected.Name,
		ChatID: request.ChatID, Category: request.Category, Content: request.Content, Confidence: 1,
		EvidenceMessageIDs: []string{},
		SourceType:         SourceManual, Importance: request.Importance, ValidFrom: request.ValidFrom,
		ValidUntil: request.ValidUntil, Pinned: request.Pinned}, nil
}

func (s *Service) scoreAndSort(claims []Claim, limit int) []Claim {
	now := s.now()
	for index := range claims {
		claims[index] = s.decorateAt(claims[index], now)
	}
	sort.SliceStable(claims, func(i, j int) bool {
		if claims[i].EffectiveScore == claims[j].EffectiveScore {
			return claims[i].LastEvidenceAt > claims[j].LastEvidenceAt
		}
		return claims[i].EffectiveScore > claims[j].EffectiveScore
	})
	if limit > 0 && len(claims) > limit {
		claims = claims[:limit]
	}
	return claims
}

func (s *Service) withStatus(claims []Claim) []Claim {
	for index := range claims {
		claims[index] = s.decorate(claims[index])
	}
	return claims
}

func (s *Service) decorate(claim Claim) Claim { return s.decorateAt(claim, s.now()) }

func (s *Service) decorateAt(claim Claim, now time.Time) Claim {
	claim.Status = ClaimStatus(claim, now)
	claim.EffectiveScore = EffectiveScore(claim, now, s.decayHalfLifeDays)
	return claim
}

func ClaimStatus(claim Claim, now time.Time) string {
	nowMillis := now.UnixMilli()
	if claim.ValidFrom > 0 && nowMillis < claim.ValidFrom {
		return "scheduled"
	}
	if claim.ValidUntil > 0 && nowMillis >= claim.ValidUntil {
		return "expired"
	}
	return "active"
}

func EffectiveScore(claim Claim, now time.Time, halfLifeDays float64) float64 {
	if ClaimStatus(claim, now) != "active" {
		return 0
	}
	if claim.SourceType == SourceManual {
		score := .9
		if claim.Importance == ImportanceImportant {
			score = .97
		} else if claim.Importance == ImportanceConstraint {
			score = 1
		}
		if claim.Pinned {
			score = min(1, score+.03)
		}
		return score
	}
	if halfLifeDays <= 0 {
		halfLifeDays = 90
	}
	reference := claim.LastEvidenceAt
	if reference <= 0 {
		reference = claim.UpdatedAt
	}
	ageDays := now.Sub(time.UnixMilli(reference)).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	score := claim.Confidence * math.Pow(.5, ageDays/halfLifeDays)
	return math.Round(max(0, min(1, score))*10000) / 10000
}
