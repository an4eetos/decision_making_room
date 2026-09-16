package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrChatSessionNotFound = errors.New("chat session not found")

type ChatSource struct {
	ID    string
	Kind  string
	Title string
	Score float64
}

type ChatSession struct {
	ID               uuid.UUID
	Title            string
	Summary          string
	SummaryUpdatedAt *time.Time
	// Tier is the session's default answer depth, so a conversation you started
	// deep stays deep without re-picking it every turn.
	Tier string
	// Generals is the session's pinned lens selection. Empty means auto-select.
	Generals []string
	// ModeID is the conversation's current mode, and ModeLocked is whether the
	// user set it by hand — a locked mode is never overridden by detection.
	ModeID     string
	ModeLocked bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ChatMessage struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	Role      string
	Content   string
	Sources   []ChatSource
	// Tier records what actually ran for this answer. Stored per message rather
	// than only per session because the depth can change mid-conversation, and
	// because it is what lets you compare answers later without adding telemetry.
	Tier string
	// Generals are the lenses this answer was written through, and DetectMethod
	// is how the mode was arrived at.
	Generals     []string
	ModeID       string
	DetectMethod string
	CreatedAt    time.Time
}

type ChatRepository interface {
	CreateSession(ctx context.Context, session ChatSession) (ChatSession, error)
	ListSessions(ctx context.Context, limit int) ([]ChatSession, error)
	GetSession(ctx context.Context, id uuid.UUID) (ChatSession, error)
	UpdateSession(ctx context.Context, session ChatSession) error
	CreateMessage(ctx context.Context, message ChatMessage) (ChatMessage, error)
	ListMessages(ctx context.Context, sessionID uuid.UUID) ([]ChatMessage, error)
	DeleteSession(ctx context.Context, id uuid.UUID) error
}
