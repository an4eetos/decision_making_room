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

// LogisticsRepository keeps what supplies the fronts: the sorties flown and
// the energy reported. Both are yours; nothing here is extracted.
type LogisticsRepository interface {
	CreateSortie(ctx context.Context, s domain.Sortie) (domain.Sortie, error)
	DeleteSortie(ctx context.Context, id uuid.UUID) error
	// ListSorties returns the sorties flown on or after a YYYY-MM-DD day,
	// latest first.
	ListSorties(ctx context.Context, since string) ([]domain.Sortie, error)
	// FirstSortieDay is the day of the first sortie on record, empty with
	// none: a supply record younger than its window is measured over what
	// there is, not diluted by days before it began.
	FirstSortieDay(ctx context.Context) (string, error)

	// SaveEnergy writes a day's reading, replacing one already there.
	SaveEnergy(ctx context.Context, r domain.EnergyReport) (domain.EnergyReport, error)
	DeleteEnergy(ctx context.Context, day string) error
	// ListEnergy returns the readings on or after a YYYY-MM-DD day, oldest
	// first.
	ListEnergy(ctx context.Context, since string) ([]domain.EnergyReport, error)
}

// DeploymentSource reads the relocation plans deployed on a front: a stay as
// a forward base with its own supply line. The relocation module implements
// it, so the campaign never imports the planner.
type DeploymentSource interface {
	Deployments(ctx context.Context) ([]domain.Deployment, error)
}

// RecordRepository keeps the quarters, the predictions filed on them, and the
// commendations sealed when a quarter closes.
type RecordRepository interface {
	CreateCampaign(ctx context.Context, c domain.Campaign) (domain.Campaign, error)
	GetCampaign(ctx context.Context, id uuid.UUID) (domain.Campaign, error)
	UpdateCampaign(ctx context.Context, c domain.Campaign) (domain.Campaign, error)
	DeleteCampaign(ctx context.Context, id uuid.UUID) error
	// ListCampaigns returns every quarter, newest quarter first.
	ListCampaigns(ctx context.Context) ([]domain.Campaign, error)

	AssignObjective(ctx context.Context, campaignID, itemID uuid.UUID) error
	UnassignObjective(ctx context.Context, campaignID, itemID uuid.UUID) error
	// ListAssignments returns the objective ids assigned to a quarter.
	ListAssignments(ctx context.Context, campaignID uuid.UUID) ([]uuid.UUID, error)

	CreatePrediction(ctx context.Context, p domain.Prediction) (domain.Prediction, error)
	GetPrediction(ctx context.Context, id uuid.UUID) (domain.Prediction, error)
	UpdatePrediction(ctx context.Context, p domain.Prediction) (domain.Prediction, error)
	// ListPredictions returns every prediction, newest first, including
	// dropped ones. The record decides which to show.
	ListPredictions(ctx context.Context) ([]domain.Prediction, error)

	// SealCampaign closes the quarter and writes its commendations in one
	// transaction. The campaign must already carry phase closed, closed_at,
	// and the sealed calibration.
	SealCampaign(ctx context.Context, c domain.Campaign, lines []domain.Commendation) (domain.Campaign, error)
	ListCommendations(ctx context.Context, campaignID uuid.UUID) ([]domain.Commendation, error)
}
