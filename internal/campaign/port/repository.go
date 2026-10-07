package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicate means a live item of the same type and fingerprint exists,
	// or an active front with the same name.
	ErrDuplicate = errors.New("already on the map")
)

type Repository interface {
	CreateFront(ctx context.Context, name string) (domain.Front, error)
	UpdateFront(ctx context.Context, f domain.Front) (domain.Front, error)
	GetFront(ctx context.Context, id uuid.UUID) (domain.Front, error)
	// ListFronts returns fronts in their display order. activeOnly leaves out
	// withdrawn ones.
	ListFronts(ctx context.Context, activeOnly bool) ([]domain.Front, error)

	CreateItem(ctx context.Context, item domain.Item) (domain.Item, error)
	GetItem(ctx context.Context, id uuid.UUID) (domain.Item, error)
	// UpdateItem writes every mutable field of the item: text, front, objective,
	// status, kind, strength, answer, due and fingerprint. resolved_at follows
	// the status.
	UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error)
	// ListItems returns items in the given statuses; no statuses means all.
	ListItems(ctx context.Context, statuses []domain.Status, limit int) ([]domain.Item, error)
}
