package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

const (
	chatSummaryTriggerMessages = 8
	chatSummaryKeepRecent      = 8
)

type Chat struct {
	repo port.ChatRepository
	llm  port.LLM
	// consult keeps RAG behavior in one place.
	consult *Consult
}

type ChatSessionDTO struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Summary   string    `json:"summary,omitempty"`
	Tier      string    `json:"tier,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ChatMessageDTO struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Sources   []ConsultSource `json:"sources,omitempty"`
	Tier      string          `json:"tier,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type ChatSessionDetail struct {
	Session  ChatSessionDTO   `json:"session"`
	Messages []ChatMessageDTO `json:"messages"`
}

func NewChat(repo port.ChatRepository, llm port.LLM, consult *Consult) *Chat {
	return &Chat{repo: repo, llm: llm, consult: consult}
}

func (c *Chat) CreateSession(ctx context.Context, title string) (ChatSessionDTO, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "New chat"
	}

	session, err := c.repo.CreateSession(ctx, port.ChatSession{Title: title})
	if err != nil {
		return ChatSessionDTO{}, err
	}
	return toChatSessionDTO(session), nil
}

func (c *Chat) ListSessions(ctx context.Context) ([]ChatSessionDTO, error) {
	sessions, err := c.repo.ListSessions(ctx, 100)
	if err != nil {
		return nil, err
	}
	out := make([]ChatSessionDTO, len(sessions))
	for i, session := range sessions {
		out[i] = toChatSessionDTO(session)
	}
	return out, nil
}

func (c *Chat) DeleteSession(ctx context.Context, sessionID string) error {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return fmt.Errorf("invalid session id")
	}
	return c.repo.DeleteSession(ctx, id)
}

func (c *Chat) GetSession(ctx context.Context, sessionID string) (ChatSessionDetail, error) {
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return ChatSessionDetail{}, fmt.Errorf("invalid session id")
	}

	session, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}
	messages, err := c.repo.ListMessages(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	return ChatSessionDetail{
		Session:  toChatSessionDTO(session),
		Messages: toChatMessagesDTO(messages),
	}, nil
}

// SendMessageInput carries the per-turn choices. Tier is optional; empty falls
// back to the session's, then to the configured default.
type SendMessageInput struct {
	SessionID string
	Question  string
	Tier      string
}

func (c *Chat) SendMessage(ctx context.Context, in SendMessageInput) (ChatSessionDetail, error) {
	sessionID, question := in.SessionID, in.Question
	id, err := uuid.Parse(sessionID)
	if err != nil {
		return ChatSessionDetail{}, fmt.Errorf("invalid session id")
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return ChatSessionDetail{}, fmt.Errorf("question is required")
	}

	session, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	messages, err := c.repo.ListMessages(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	userMessage, err := c.repo.CreateMessage(ctx, port.ChatMessage{
		SessionID: id,
		Role:      "user",
		Content:   question,
	})
	if err != nil {
		return ChatSessionDetail{}, err
	}
	messages = append(messages, userMessage)

	if strings.TrimSpace(session.Title) == "" || session.Title == "New chat" {
		session.Title = deriveChatTitle(question)
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	// Per-turn tier wins, then the session's, then the configured default. An
	// explicit pick also becomes the session default, so a conversation you took
	// deep stays deep without re-picking every turn.
	tier := strings.TrimSpace(in.Tier)
	if tier == "" {
		tier = session.Tier
	} else if tier != session.Tier {
		session.Tier = tier
		if err := c.repo.UpdateSession(ctx, session); err != nil {
			return ChatSessionDetail{}, err
		}
	}

	conversationHistory := buildChatHistory(session.Summary, messages[:len(messages)-1])
	consultResult, err := c.consult.Execute(ctx, ConsultInput{
		Question: question,
		History:  conversationHistory,
		Tier:     tier,
	})
	if err != nil {
		return ChatSessionDetail{}, err
	}

	assistantMessage, err := c.repo.CreateMessage(ctx, port.ChatMessage{
		SessionID: id,
		Role:      "assistant",
		Content:   consultResult.Answer,
		Sources:   toChatSources(consultResult.Sources),
		Tier:      consultResult.Tier,
	})
	if err != nil {
		return ChatSessionDetail{}, err
	}
	messages = append(messages, assistantMessage)

	if err := c.maybeSummarize(ctx, &session, messages); err != nil {
		return ChatSessionDetail{}, err
	}

	updatedSession, err := c.repo.GetSession(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}
	updatedMessages, err := c.repo.ListMessages(ctx, id)
	if err != nil {
		return ChatSessionDetail{}, err
	}

	return ChatSessionDetail{
		Session:  toChatSessionDTO(updatedSession),
		Messages: toChatMessagesDTO(updatedMessages),
	}, nil
}

func (c *Chat) maybeSummarize(ctx context.Context, session *port.ChatSession, messages []port.ChatMessage) error {
	if len(messages) < chatSummaryTriggerMessages {
		return nil
	}

	cutoff := len(messages) - chatSummaryKeepRecent
	if cutoff <= 0 {
		return nil
	}

	summaryPrompt := buildSummaryPrompt(session.Summary, messages[:cutoff])
	answer, err := c.llm.Chat(ctx, []port.Message{
		{Role: "system", Content: "Summarize the conversation so future answers can preserve user intent, commitments, preferences, and unresolved questions. Be concise and factual."},
		{Role: "user", Content: summaryPrompt},
	})
	if err != nil {
		return nil
	}
	answer = strings.TrimSpace(answer)
	if answer == "" || answer == session.Summary {
		return nil
	}

	now := time.Now().UTC()
	session.Summary = answer
	session.SummaryUpdatedAt = &now
	return c.repo.UpdateSession(ctx, *session)
}

func buildChatHistory(summary string, messages []port.ChatMessage) []port.Message {
	var history []port.Message

	if strings.TrimSpace(summary) != "" {
		history = append(history, port.Message{
			Role:    "user",
			Content: "Earlier conversation summary:\n" + strings.TrimSpace(summary),
		})
	}

	start := 0
	if len(messages) > chatSummaryKeepRecent {
		start = len(messages) - chatSummaryKeepRecent
	}

	for _, message := range messages[start:] {
		role := "assistant"
		if message.Role == "user" {
			role = "user"
		}
		history = append(history, port.Message{
			Role:    role,
			Content: strings.TrimSpace(message.Content),
		})
	}

	return history
}

func buildSummaryPrompt(existingSummary string, messages []port.ChatMessage) string {
	var b strings.Builder
	if strings.TrimSpace(existingSummary) != "" {
		b.WriteString("Existing summary:\n")
		b.WriteString(strings.TrimSpace(existingSummary))
		b.WriteString("\n\n")
	}
	b.WriteString("New conversation turns:\n")
	for _, message := range messages {
		role := "Assistant"
		if message.Role == "user" {
			role = "User"
		}
		fmt.Fprintf(&b, "%s: %s\n", role, strings.TrimSpace(message.Content))
	}
	return b.String()
}

func deriveChatTitle(question string) string {
	question = strings.TrimSpace(question)
	if len(question) <= 60 {
		return question
	}
	return question[:60] + "..."
}

func toChatSessionDTO(session port.ChatSession) ChatSessionDTO {
	return ChatSessionDTO{
		ID:        session.ID.String(),
		Title:     session.Title,
		Summary:   session.Summary,
		Tier:      session.Tier,
		CreatedAt: session.CreatedAt,
		UpdatedAt: session.UpdatedAt,
	}
}

func toChatMessagesDTO(messages []port.ChatMessage) []ChatMessageDTO {
	out := make([]ChatMessageDTO, len(messages))
	for i, message := range messages {
		out[i] = ChatMessageDTO{
			ID:        message.ID.String(),
			Role:      message.Role,
			Content:   message.Content,
			Sources:   fromChatSources(message.Sources),
			Tier:      message.Tier,
			CreatedAt: message.CreatedAt,
		}
	}
	return out
}

func toChatSources(sources []ConsultSource) []port.ChatSource {
	out := make([]port.ChatSource, len(sources))
	for i, source := range sources {
		out[i] = port.ChatSource{
			ID:    source.ID,
			Kind:  string(source.Kind),
			Title: source.Title,
			Score: source.Score,
		}
	}
	return out
}

func fromChatSources(sources []port.ChatSource) []ConsultSource {
	out := make([]ConsultSource, len(sources))
	for i, source := range sources {
		out[i] = ConsultSource{
			ID:    source.ID,
			Kind:  domain.MemoryKind(source.Kind),
			Title: source.Title,
			Score: source.Score,
		}
	}
	return out
}
