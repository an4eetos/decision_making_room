package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/beast/domain"
)

var ErrNotFound = errors.New("beast task not found")

type Repository interface {
	// Create numbers the task after the session's last one.
	Create(ctx context.Context, t domain.Task) (domain.Task, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Task, error)
	// ListBySession returns every task of the session in number order.
	ListBySession(ctx context.Context, sessionID uuid.UUID) ([]domain.Task, error)
	// Update writes text, minutes, deadline and status. Resolving stamps
	// resolved_at.
	Update(ctx context.Context, t domain.Task) (domain.Task, error)
	// Rearm counts a missed deadline and sets the new one, but only if the task
	// is still open and still past its deadline at now. ok is false when
	// another request got there first.
	Rearm(ctx context.Context, id uuid.UUID, now, deadline time.Time) (t domain.Task, ok bool, err error)
}

// Loop is an open loop as the board sees it.
type Loop struct {
	ID    uuid.UUID
	Text  string
	DueAt *time.Time
}

// Loops is the open-loop list: where a crisis comes from, and what a finished
// order closes.
type Loops interface {
	// DueBy lists the live loops due at or before the given moment, soonest
	// first.
	DueBy(ctx context.Context, by time.Time) ([]Loop, error)
	Done(ctx context.Context, id uuid.UUID) error
}
