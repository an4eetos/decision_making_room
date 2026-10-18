package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

const maxCampaignName = 80

const maxCampaignNote = 2000

var (
	// ErrClosed is a change to a quarter that has already been sealed.
	ErrClosed = errors.New("that quarter is closed")
	// ErrPhase is a phase change the quarter's life does not allow.
	ErrPhase = errors.New("that phase change does not apply")
	// ErrNotObjective is an assignment that is not an objective on the map.
	ErrNotObjective = errors.New("only an objective on the map is assigned to a quarter")
	// ErrPrediction is a change a prediction's state does not allow.
	ErrPrediction = errors.New("that change does not apply to this prediction")
)

// Record keeps the quarters. Nothing here is inferred from what you write:
// you open the quarter, assign the objectives, state the chances and score
// the predictions.
type Record struct {
	repo  port.RecordRepository
	items port.Repository
	loc   *time.Location
	now   func() time.Time

	consult Answerer
	busy    chan struct{}
	mu      sync.Mutex
	review  ReviewState
}

// ReviewState is what the page polls while an after-action is being written.
type ReviewState struct {
	Busy       bool
	Since      time.Time
	CampaignID uuid.UUID
	Error      string
}

func NewRecord(repo port.RecordRepository, items port.Repository, loc *time.Location) *Record {
	if loc == nil {
		loc = time.Local
	}
	return &Record{repo: repo, items: items, loc: loc, now: time.Now, busy: make(chan struct{}, 1)}
}

// WithConsult is the model the after-action is written with. Without it the
// record still opens, scores and seals; only the write-up is refused.
func (u *Record) WithConsult(c Answerer) *Record {
	u.consult = c
	return u
}

// OpenInput names a quarter. Both fields empty means this quarter, titled
// with its own label.
type OpenInput struct {
	Name    string
	Quarter string
}

// Open starts a quarter in planning. One campaign per quarter.
func (u *Record) Open(ctx context.Context, in OpenInput) (domain.Campaign, error) {
	now := u.now().In(u.loc)
	quarter := strings.TrimSpace(in.Quarter)
	if quarter == "" {
		quarter = domain.QuarterOf(now)
	}
	if !domain.ValidQuarter(quarter) {
		return domain.Campaign{}, errors.New("a quarter looks like 2026-Q4")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = domain.QuarterLabel(quarter)
	}
	if len([]rune(name)) > maxCampaignName {
		return domain.Campaign{}, fmt.Errorf("keep the name under %d characters", maxCampaignName)
	}
	return u.repo.CreateCampaign(ctx, domain.Campaign{
		Quarter: quarter, Name: name, Phase: domain.PhasePlanning, OpenedAt: now,
	})
}

// Advance moves a quarter along its life. Closing seals the commendations
// and the calibration; review can go back to operations.
func (u *Record) Advance(ctx context.Context, id uuid.UUID, to domain.Phase) (domain.Campaign, error) {
	if !to.Valid() {
		return domain.Campaign{}, fmt.Errorf("unknown phase %q", to)
	}
	c, err := u.repo.GetCampaign(ctx, id)
	if err != nil {
		return domain.Campaign{}, err
	}
	if c.Phase == domain.PhaseClosed {
		return domain.Campaign{}, ErrClosed
	}
	if !phaseAllowed(c.Phase, to) {
		return domain.Campaign{}, fmt.Errorf("%w: %s to %s", ErrPhase, c.Phase, to)
	}
	if to == domain.PhaseClosed {
		return u.seal(ctx, c)
	}
	c.Phase = to
	return u.repo.UpdateCampaign(ctx, c)
}

func phaseAllowed(from, to domain.Phase) bool {
	switch from {
	case domain.PhasePlanning:
		return to == domain.PhaseOperations
	case domain.PhaseOperations:
		return to == domain.PhaseReview
	case domain.PhaseReview:
		return to == domain.PhaseOperations || to == domain.PhaseClosed
	default:
		return false
	}
}

// SetNote stores your own line on an open quarter.
func (u *Record) SetNote(ctx context.Context, id uuid.UUID, note string) (domain.Campaign, error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > maxCampaignNote {
		return domain.Campaign{}, fmt.Errorf("keep the note under %d characters", maxCampaignNote)
	}
	c, err := u.editable(ctx, id)
	if err != nil {
		return domain.Campaign{}, err
	}
	c.Note = note
	return u.repo.UpdateCampaign(ctx, c)
}

// Delete removes a quarter that is not closed. Predictions filed on it
// stay, unfiled.
func (u *Record) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := u.editable(ctx, id); err != nil {
		return err
	}
	return u.repo.DeleteCampaign(ctx, id)
}

// Assign puts an objective that is already on the map onto the quarter.
// Assigning it twice is the same as assigning it once.
func (u *Record) Assign(ctx context.Context, campaignID, itemID uuid.UUID) error {
	if _, err := u.editable(ctx, campaignID); err != nil {
		return err
	}
	if err := u.objectiveOnMap(ctx, itemID); err != nil {
		return err
	}
	err := u.repo.AssignObjective(ctx, campaignID, itemID)
	if errors.Is(err, port.ErrDuplicate) {
		return nil
	}
	return err
}

// Unassign takes an objective off an open quarter.
func (u *Record) Unassign(ctx context.Context, campaignID, itemID uuid.UUID) error {
	if _, err := u.editable(ctx, campaignID); err != nil {
		return err
	}
	return u.repo.UnassignObjective(ctx, campaignID, itemID)
}

func (u *Record) editable(ctx context.Context, id uuid.UUID) (domain.Campaign, error) {
	c, err := u.repo.GetCampaign(ctx, id)
	if err != nil {
		return domain.Campaign{}, err
	}
	if c.Phase == domain.PhaseClosed {
		return domain.Campaign{}, ErrClosed
	}
	return c, nil
}

func (u *Record) objectiveOnMap(ctx context.Context, id uuid.UUID) error {
	item, err := u.items.GetItem(ctx, id)
	if err != nil {
		return err
	}
	if item.Type != domain.TypeObjective || !onMap(item.Status) {
		return ErrNotObjective
	}
	return nil
}

func onMap(status domain.Status) bool {
	switch status {
	case domain.StatusActive, domain.StatusResolved, domain.StatusWithdrawn:
		return true
	default:
		return false
	}
}
