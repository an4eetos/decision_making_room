package usecase

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/service"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

const preMortemModeID = "pre_mortem"

const preMortemSource = "pre_mortem"

// PredictionPatch is a partial edit. Nil fields are left alone.
type PredictionPatch struct {
	Status         *domain.PredictionStatus
	Chance         *domain.Chance
	ClearChance    bool
	Outcome        *domain.Outcome
	ClearOutcome   bool
	CampaignID     *uuid.UUID
	ClearCampaign  bool
	ObjectiveID    *uuid.UUID
	ClearObjective bool
}

// PatchPrediction keeps, drops, aims, or scores a prediction. A closed
// quarter's predictions are left as they were sealed.
func (u *Record) PatchPrediction(ctx context.Context, id uuid.UUID, patch PredictionPatch) (domain.Prediction, error) {
	pred, err := u.repo.GetPrediction(ctx, id)
	if err != nil {
		return domain.Prediction{}, err
	}
	if err := u.rejectClosed(ctx, pred.CampaignID); err != nil {
		return domain.Prediction{}, err
	}
	if err := applyPrediction(&pred, patch, u.now()); err != nil {
		return domain.Prediction{}, err
	}
	if err := u.rejectClosed(ctx, pred.CampaignID); err != nil {
		return domain.Prediction{}, err
	}
	if err := u.checkObjective(ctx, pred.ObjectiveID); err != nil {
		return domain.Prediction{}, err
	}
	return u.repo.UpdatePrediction(ctx, pred)
}

func (u *Record) rejectClosed(ctx context.Context, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	c, err := u.repo.GetCampaign(ctx, *id)
	if err != nil {
		return err
	}
	if c.Phase == domain.PhaseClosed {
		return ErrClosed
	}
	return nil
}

func (u *Record) checkObjective(ctx context.Context, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	return u.objectiveOnMap(ctx, *id)
}

func applyPrediction(pred *domain.Prediction, patch PredictionPatch, now time.Time) error {
	if err := applyStatus(pred, patch.Status); err != nil {
		return err
	}
	if err := applyChance(pred, patch); err != nil {
		return err
	}
	if err := applyOutcome(pred, patch, now); err != nil {
		return err
	}
	if patch.ClearCampaign {
		pred.CampaignID = nil
	}
	if patch.CampaignID != nil {
		pred.CampaignID = patch.CampaignID
	}
	if patch.ClearObjective {
		pred.ObjectiveID = nil
	}
	if patch.ObjectiveID != nil {
		pred.ObjectiveID = patch.ObjectiveID
	}
	if pred.Status == domain.PredictionDropped {
		pred.Chance, pred.Outcome, pred.ScoredAt = domain.ChanceNone, domain.OutcomeNone, nil
	}
	return nil
}

func applyStatus(pred *domain.Prediction, status *domain.PredictionStatus) error {
	if status == nil || *status == pred.Status {
		return nil
	}
	if pred.Status != domain.PredictionProposed || (*status != domain.PredictionKept && *status != domain.PredictionDropped) {
		return ErrPrediction
	}
	pred.Status = *status
	return nil
}

func applyChance(pred *domain.Prediction, patch PredictionPatch) error {
	if !patch.ClearChance && patch.Chance == nil {
		return nil
	}
	if pred.Status != domain.PredictionKept {
		return ErrPrediction
	}
	if patch.ClearChance {
		pred.Chance = domain.ChanceNone
		return nil
	}
	if !patch.Chance.Valid() || *patch.Chance == domain.ChanceNone {
		return errors.New("a chance is long, even or likely")
	}
	pred.Chance = *patch.Chance
	return nil
}

func applyOutcome(pred *domain.Prediction, patch PredictionPatch, now time.Time) error {
	if !patch.ClearOutcome && patch.Outcome == nil {
		return nil
	}
	if pred.Status != domain.PredictionKept {
		return ErrPrediction
	}
	if patch.ClearOutcome {
		pred.Outcome, pred.ScoredAt = domain.OutcomeNone, nil
		return nil
	}
	if !patch.Outcome.Valid() || *patch.Outcome == domain.OutcomeNone {
		return errors.New("an outcome is called or missed")
	}
	pred.Outcome = *patch.Outcome
	pred.ScoredAt = &now
	return nil
}

// ObserveTurn implements memport.TurnObserver. A pre-mortem's failures are
// read out of the answer; every other turn is ignored. It returns immediately.
func (u *Record) ObserveTurn(turn memport.Turn) {
	if turn.ModeID != preMortemModeID {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := u.FromPreMortem(ctx, turn); err != nil {
			log.Printf("campaign: reading pre-mortem: %v", err)
		}
	}()
}

// FromPreMortem files the failures a pre-mortem named as proposals. Exported
// for tests. A duplicate of one already on the record is skipped.
func (u *Record) FromPreMortem(ctx context.Context, turn memport.Turn) error {
	intel := service.ParsePreMortem(turn.AssistantText)
	if len(intel.Failures) == 0 {
		return nil
	}
	campaign, err := u.filingCampaign(ctx)
	if err != nil {
		return err
	}
	objectives, err := u.aimTargets(ctx, campaign)
	if err != nil {
		return err
	}
	for _, failure := range intel.Failures {
		if err := u.proposeFailure(ctx, failure, campaign, objectives, turn); err != nil && !errors.Is(err, port.ErrDuplicate) {
			return err
		}
	}
	return nil
}

// filingCampaign is today's quarter when it is still open, including review.
// A closed quarter is not written to. No open quarter leaves the proposal unfiled.
func (u *Record) filingCampaign(ctx context.Context) (*domain.Campaign, error) {
	quarter := domain.QuarterOf(u.now().In(u.loc))
	campaigns, err := u.repo.ListCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	for i := range campaigns {
		if campaigns[i].Quarter == quarter && campaigns[i].Phase != domain.PhaseClosed {
			return &campaigns[i], nil
		}
	}
	return nil, nil
}

func (u *Record) aimTargets(ctx context.Context, campaign *domain.Campaign) ([]domain.Item, error) {
	items, err := u.items.ListItems(ctx, []domain.Status{domain.StatusActive, domain.StatusResolved, domain.StatusWithdrawn}, 500)
	if err != nil {
		return nil, err
	}
	objectives := objectivesOf(items)
	if campaign == nil {
		return activeOnly(objectives), nil
	}
	ids, err := u.repo.ListAssignments(ctx, campaign.ID)
	if err != nil {
		return nil, err
	}
	if assigned := filterIDs(objectives, ids); len(assigned) > 0 {
		return assigned, nil
	}
	return activeOnly(objectives), nil
}

func (u *Record) proposeFailure(ctx context.Context, failure service.Failure, campaign *domain.Campaign, objectives []domain.Item, turn memport.Turn) error {
	pred := domain.Prediction{
		Text: failure.Text, Warning: failure.Warning, Status: domain.PredictionProposed,
		Source: preMortemSource, Fingerprint: service.Fingerprint(failure.Text),
	}
	if turn.SessionID != uuid.Nil {
		pred.SessionID = &turn.SessionID
	}
	if turn.MessageID != uuid.Nil {
		pred.MessageID = &turn.MessageID
	}
	if campaign != nil {
		pred.CampaignID = &campaign.ID
	}
	if target := aimPrediction(failure.Text, objectives); target != nil {
		pred.ObjectiveID = &target.ID
	}
	_, err := u.repo.CreatePrediction(ctx, pred)
	return err
}

func aimPrediction(text string, objectives []domain.Item) *domain.Item {
	var target *domain.Item
	best := 0
	for i := range objectives {
		if n := service.Overlap(text, objectives[i].Text); n > best {
			best = n
			target = &objectives[i]
		}
	}
	return target
}

func objectivesOf(items []domain.Item) []domain.Item {
	var out []domain.Item
	for _, it := range items {
		if it.Type == domain.TypeObjective {
			out = append(out, it)
		}
	}
	return out
}

func activeOnly(items []domain.Item) []domain.Item {
	var out []domain.Item
	for _, it := range items {
		if it.Status == domain.StatusActive {
			out = append(out, it)
		}
	}
	return out
}

func filterIDs(items []domain.Item, ids []uuid.UUID) []domain.Item {
	want := map[uuid.UUID]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []domain.Item
	for _, it := range items {
		if want[it.ID] {
			out = append(out, it)
		}
	}
	return out
}
