package port

import (
	"context"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

type PlanRepository interface {
	CreatePlan(ctx context.Context, plan domain.Plan) (domain.Plan, error)
	UpdatePlan(ctx context.Context, plan domain.Plan) (domain.Plan, error)
	GetPlan(ctx context.Context, id uuid.UUID) (domain.Plan, error)
	ListPlans(ctx context.Context) ([]domain.Plan, error)
	DeletePlan(ctx context.Context, id uuid.UUID) error

	// ReplaceItems upserts by (plan_id, catalog_id) and removes catalogue lines
	// that no longer apply. Lines you added yourself, and any status or price you
	// set, survive a rebuild — otherwise changing the departure date would wipe
	// the work of ticking things off.
	ReplaceItems(ctx context.Context, planID uuid.UUID, items []domain.Item) error
	GetItem(ctx context.Context, id uuid.UUID) (domain.Item, error)
	UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error)
	AddItem(ctx context.Context, item domain.Item) (domain.Item, error)
	DeleteItem(ctx context.Context, id uuid.UUID) error

	ReplacePitfalls(ctx context.Context, planID uuid.UUID, pitfalls []domain.PlanPitfall) error
	AcknowledgePitfall(ctx context.Context, id uuid.UUID, acknowledged bool) error
}

// PriceRepository remembers what things actually cost where, so a returning stay
// is priced from experience rather than from an estimate.
type PriceRepository interface {
	Record(ctx context.Context, catalogID, destination string, unitCost float64, currency string) error
	// Lookup returns the most recent observed price per catalogue id for a
	// destination.
	Lookup(ctx context.Context, destination string, catalogIDs []string) (map[string]Observation, error)
}

type Observation struct {
	UnitCost float64
	Currency string
}

var ErrNotFound = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "relocation: not found" }
