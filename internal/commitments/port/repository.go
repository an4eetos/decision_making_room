package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
)

var (
	ErrNotFound = errors.New("commitment not found")
	// ErrDuplicate means a live commitment with the same fingerprint exists.
	ErrDuplicate = errors.New("commitment already tracked")
)

type Repository interface {
	Create(ctx context.Context, c domain.Commitment) (domain.Commitment, error)
	Get(ctx context.Context, id uuid.UUID) (domain.Commitment, error)
	// List returns commitments in the given statuses, newest-touched first.
	// No statuses means every one.
	List(ctx context.Context, statuses []domain.Status, limit int) ([]domain.Commitment, error)
	SetStatus(ctx context.Context, id uuid.UUID, status domain.Status) (domain.Commitment, error)
	UpdateText(ctx context.Context, id uuid.UUID, text, fingerprint string, due *time.Time) (domain.Commitment, error)
	// SetTarget aims an order at a campaign item, or with nil at nothing.
	SetTarget(ctx context.Context, id uuid.UUID, target *uuid.UUID, kind domain.Kind) (domain.Commitment, error)
	// ListByTarget returns the live and recently finished orders aimed at any of
	// the given campaign items.
	ListByTargets(ctx context.Context, targets []uuid.UUID) ([]domain.Commitment, error)

	// MarkStale moves open commitments untouched since before the cutoff to
	// stale, and returns the ones it moved so a nudge can name them.
	MarkStale(ctx context.Context, cutoff time.Time) ([]domain.Commitment, error)
}

// ObjectiveRef is an active objective an extracted commitment can be linked to.
type ObjectiveRef struct {
	ID   uuid.UUID
	Text string
}

// ObjectiveSource lists the objectives on the campaign map, so extraction can
// say which one a new commitment serves. Optional: without it commitments are
// extracted exactly as before.
type ObjectiveSource interface {
	ActiveObjectives(ctx context.Context) ([]ObjectiveRef, error)
}

// StaleListener hears about commitments at the moment they go stale.
//
// The moment matters: a nudge fired from the transition names each stale item
// once. A nudge that re-read the stale list every day would nag about the same
// thing forever, which is exactly what marking it stale was meant to stop.
type StaleListener interface {
	OnStale(ctx context.Context, newlyStale []domain.Commitment)
}
