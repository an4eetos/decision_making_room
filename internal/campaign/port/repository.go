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

// SnapshotRepository keeps one theatre per day for the timelapse.
type SnapshotRepository interface {
	// SaveSnapshot writes the day's snapshot, replacing the one already there.
	SaveSnapshot(ctx context.Context, s domain.Snapshot) error
	// GetSnapshot returns the snapshot for a YYYY-MM-DD day.
	GetSnapshot(ctx context.Context, day string) (domain.Snapshot, error)
	// GetSnapshotBefore returns the latest snapshot taken before a YYYY-MM-DD
	// day: yesterday's map, usually. One row, because it is read on every
	// answer. ErrNotFound when there is none.
	GetSnapshotBefore(ctx context.Context, day string) (domain.Snapshot, error)
	// ListSnapshots returns the most recent snapshots, oldest first.
	ListSnapshots(ctx context.Context, limit int) ([]domain.Snapshot, error)
}

// AppraisalRepository keeps what the staff said, with the date it said it.
type AppraisalRepository interface {
	CreateAppraisal(ctx context.Context, a domain.Appraisal) (domain.Appraisal, error)
	GetAppraisal(ctx context.Context, id uuid.UUID) (domain.Appraisal, error)
	// ListAppraisals returns the latest first.
	ListAppraisals(ctx context.Context, limit int) ([]domain.Appraisal, error)
	AttachAppraisalSession(ctx context.Context, id, sessionID uuid.UUID) error
}
