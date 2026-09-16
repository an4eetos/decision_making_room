package usecase

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

type stubChatRepo struct {
	deleted uuid.UUID
}

func (s *stubChatRepo) CreateSession(ctx context.Context, session port.ChatSession) (port.ChatSession, error) {
	return session, nil
}

func (s *stubChatRepo) ListSessions(ctx context.Context, limit int) ([]port.ChatSession, error) {
	return nil, nil
}

func (s *stubChatRepo) GetSession(ctx context.Context, id uuid.UUID) (port.ChatSession, error) {
	return port.ChatSession{}, nil
}

func (s *stubChatRepo) UpdateSession(ctx context.Context, session port.ChatSession) error {
	return nil
}

func (s *stubChatRepo) CreateMessage(ctx context.Context, message port.ChatMessage) (port.ChatMessage, error) {
	return message, nil
}

func (s *stubChatRepo) ListMessages(ctx context.Context, sessionID uuid.UUID) ([]port.ChatMessage, error) {
	return nil, nil
}

func (s *stubChatRepo) DeleteSession(ctx context.Context, id uuid.UUID) error {
	s.deleted = id
	return nil
}

func TestDeleteSession(t *testing.T) {
	t.Parallel()

	repo := &stubChatRepo{}
	chat := NewChat(repo, nil, nil)
	sessionID := uuid.New().String()

	if err := chat.DeleteSession(context.Background(), sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if repo.deleted.String() != sessionID {
		t.Fatalf("deleted = %s, want %s", repo.deleted, sessionID)
	}
}

func TestDeleteSessionInvalidID(t *testing.T) {
	t.Parallel()

	chat := NewChat(&stubChatRepo{}, nil, nil)
	err := chat.DeleteSession(context.Background(), "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "invalid session id") {
		t.Fatalf("expected invalid session id error, got %v", err)
	}
}

func TestBuildChatHistoryIncludesSummaryAndRecentTurns(t *testing.T) {
	t.Parallel()

	messages := []port.ChatMessage{
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "recent question"},
		{Role: "assistant", Content: "recent answer"},
	}

	history := buildChatHistory("prior topics discussed", messages)

	if len(history) != 5 {
		t.Fatalf("expected 5 history messages, got %d", len(history))
	}
	if !strings.Contains(history[0].Content, "Earlier conversation summary:") {
		t.Fatalf("expected summary prefix, got %q", history[0].Content)
	}
	if history[1].Role != "user" || history[1].Content != "old question" {
		t.Fatalf("unexpected first turn: %+v", history[1])
	}
	if history[4].Role != "assistant" || history[4].Content != "recent answer" {
		t.Fatalf("unexpected last turn: %+v", history[4])
	}
}

func TestBuildChatHistoryAppliesRollingWindow(t *testing.T) {
	t.Parallel()

	messages := make([]port.ChatMessage, 0, 10)
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, port.ChatMessage{
			ID:      uuid.New(),
			Role:    role,
			Content: "message",
		})
	}

	history := buildChatHistory("", messages)
	if len(history) != chatSummaryKeepRecent {
		t.Fatalf("expected %d recent messages, got %d", chatSummaryKeepRecent, len(history))
	}
}

func TestBuildChatHistoryEmpty(t *testing.T) {
	t.Parallel()

	history := buildChatHistory("", nil)
	if len(history) != 0 {
		t.Fatalf("expected empty history, got %d messages", len(history))
	}
}
