package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/checkin/domain"
)

var (
	ErrNotFound = errors.New("check-in not found")
	// ErrAlreadyFired means this slot already has a check-in for this day. It is
	// the expected outcome of a restart or a second tick, not a failure.
	ErrAlreadyFired = errors.New("check-in already fired for this slot today")
)

type Repository interface {
	Create(ctx context.Context, c domain.CheckIn) (domain.CheckIn, error)
	Exists(ctx context.Context, slot string, localDate time.Time) (bool, error)
	Get(ctx context.Context, id uuid.UUID) (domain.CheckIn, error)
	Unseen(ctx context.Context, limit int) ([]domain.CheckIn, error)
	MarkSeen(ctx context.Context, id uuid.UUID) error
	AttachSession(ctx context.Context, id, sessionID uuid.UUID) error
}
