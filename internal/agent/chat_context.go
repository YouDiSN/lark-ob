package agent

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	appmodel "github.com/youdisn/lark-ob/internal/model"
)

type chatContextRepository interface {
	GetAgentChatContext(context.Context, string) (appmodel.AgentChatContext, error)
	SaveAgentChatContext(context.Context, appmodel.AgentChatContext) error
}

// EnqueueChatContext coalesces repeated poll results for the same chat. The
// actual model work happens outside the sync path, so inbox refreshes stay fast.
func (e *Engine) EnqueueChatContext(chatID string) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" || e.contextQueue == nil {
		return
	}
	e.contextMu.Lock()
	if e.contextQueued[chatID] {
		e.contextMu.Unlock()
		return
	}
	e.contextQueued[chatID] = true
	e.contextMu.Unlock()
	select {
	case e.contextQueue <- chatID:
	default:
		e.contextMu.Lock()
		delete(e.contextQueued, chatID)
		e.contextMu.Unlock()
	}
}

// RunChatContextWorkers maintains independent durable contexts per chat.
func (e *Engine) RunChatContextWorkers(ctx context.Context, workers int) {
	if workers <= 0 {
		workers = 1
	}
	for worker := 0; worker < workers; worker++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case chatID := <-e.contextQueue:
					updated, _ := e.UpdateChatContext(ctx, chatID)
					e.contextMu.Lock()
					delete(e.contextQueued, chatID)
					e.contextMu.Unlock()
					// A message can land while the model is summarizing. Requeue only
					// when we have a valid saved cursor and the chat advanced past it.
					if updated.ChatID != "" {
						if chat, err := e.repo.Chat(ctx, chatID); err == nil && chatCursorAfter(chat.LastTime, chat.LastPosition, updated.SourceLastTime, updated.SourceLastPosition) {
							e.EnqueueChatContext(chatID)
						}
					}
				}
			}
		}()
	}
}

func (e *Engine) UpdateChatContext(ctx context.Context, chatID string) (appmodel.AgentChatContext, error) {
	repository, ok := e.repo.(chatContextRepository)
	if !ok {
		return appmodel.AgentChatContext{}, fmt.Errorf("AgentContext 仓库未启用")
	}
	chat, err := e.repo.Chat(ctx, chatID)
	if err != nil {
		return appmodel.AgentChatContext{}, err
	}
	previous, previousErr := repository.GetAgentChatContext(ctx, chatID)
	if previousErr != nil && previousErr != sql.ErrNoRows {
		return appmodel.AgentChatContext{}, previousErr
	}
	if previousErr == nil && !chatCursorAfter(chat.LastTime, chat.LastPosition, previous.SourceLastTime, previous.SourceLastPosition) {
		return previous, nil
	}
	messages, err := e.repo.Messages(ctx, chatID, 60)
	if err != nil {
		return appmodel.AgentChatContext{}, err
	}
	if len(messages) == 0 {
		return appmodel.AgentChatContext{}, fmt.Errorf("当前会话没有消息")
	}
	last := messages[len(messages)-1]
	participantSet := map[string]bool{}
	participants := make([]string, 0)
	for _, message := range messages {
		if message.SenderName != "" && !participantSet[message.SenderName] {
			participantSet[message.SenderName] = true
			participants = append(participants, message.SenderName)
		}
	}
	value := appmodel.AgentChatContext{
		ChatID: chatID, Summary: fallbackChatSummary(messages), Topics: recentTopics(messages, 4), Participants: participants,
		ReplyTargetMessageID: fallbackReplyTargetID(messages), SourceLastTime: last.CreatedAt,
		SourceLastPosition: last.Position, Model: "local-context-v1", UpdatedAt: time.Now().UnixMilli(),
	}
	if err := repository.SaveAgentChatContext(ctx, value); err != nil {
		return value, err
	}
	return value, nil
}

func chatCursorAfter(timeA, positionA, timeB, positionB int64) bool {
	return timeA > timeB || (timeA == timeB && positionA > positionB)
}

func fallbackChatSummary(messages []appmodel.Message) string {
	start := len(messages) - 12
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, message := range messages[start:] {
		content := []rune(strings.TrimSpace(message.Content))
		if len(content) > 120 {
			content = content[:120]
		}
		if len(content) > 0 {
			lines = append(lines, message.SenderName+"："+string(content))
		}
	}
	return strings.Join(lines, "\n")
}

func recentTopics(messages []appmodel.Message, limit int) []string {
	seen := map[string]bool{}
	topics := make([]string, 0, limit)
	for index := len(messages) - 1; index >= 0 && len(topics) < limit; index-- {
		content := []rune(strings.TrimSpace(messages[index].Content))
		if len(content) == 0 {
			continue
		}
		if len(content) > 60 {
			content = content[:60]
		}
		text := string(content)
		if !seen[text] {
			seen[text] = true
			topics = append(topics, text)
		}
	}
	return topics
}

func fallbackReplyTargetID(messages []appmodel.Message) string {
	target, _ := selectReplyTarget(messages, "")
	if target != nil {
		return target.MessageID
	}
	return ""
}
