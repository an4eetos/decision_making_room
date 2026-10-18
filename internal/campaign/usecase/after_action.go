package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

const afterActionModeID = "after_action"

// reviewTimeout bounds a background after-action. A standard call is bounded
// at four minutes by the consult; this is the backstop above that.
const reviewTimeout = 6 * time.Minute

var (
	// ErrReviewBusy means an after-action is already being written.
	ErrReviewBusy = errors.New("an after-action is already being written")
	// ErrNotInReview means the quarter is not in its after-action yet.
	ErrNotInReview = errors.New("open the after-action once the quarter is in review")
)

// StartReview writes the after-action in the background and returns at once.
// The page polls Review on the record. Only a quarter in review can be written,
// and closing does not require that it was.
func (u *Record) StartReview(ctx context.Context, id uuid.UUID) error {
	select {
	case u.busy <- struct{}{}:
	default:
		return ErrReviewBusy
	}
	rec, err := u.recordOf(ctx, id)
	if err != nil || rec.Campaign.Phase != domain.PhaseReview {
		<-u.busy
		if err != nil {
			return err
		}
		return ErrNotInReview
	}
	if u.consult == nil {
		<-u.busy
		return errors.New("no model is configured to write an after-action")
	}

	u.mu.Lock()
	u.review = ReviewState{Busy: true, Since: u.now(), CampaignID: id}
	u.mu.Unlock()

	go u.runReview(id, rec)
	return nil
}

func (u *Record) runReview(id uuid.UUID, rec CampaignRecord) {
	defer func() { <-u.busy }()
	ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
	defer cancel()
	err := u.writeReview(ctx, rec)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.review = ReviewState{}
	if err != nil {
		log.Printf("campaign: after-action: %v", err)
		u.review.Error = err.Error()
		u.review.CampaignID = id
	}
}

func (u *Record) writeReview(ctx context.Context, rec CampaignRecord) error {
	result, err := u.consult.Execute(ctx, memusecase.ConsultInput{
		Question: reviewQuestion(rec),
		Tier:     "standard",
		ModeID:   afterActionModeID,
	})
	if err != nil {
		return fmt.Errorf("after-action: %w", err)
	}
	body := strings.TrimSpace(result.Answer)
	if body == "" {
		return errors.New("the after-action came back empty")
	}
	c, err := u.repo.GetCampaign(ctx, rec.Campaign.ID)
	if err != nil {
		return err
	}
	if c.Phase != domain.PhaseReview {
		return errors.New("the quarter left review before the after-action was written")
	}
	now := u.now()
	c.Review, c.ReviewAt = body, &now
	_, err = u.repo.UpdateCampaign(ctx, c)
	return err
}

func (u *Record) recordOf(ctx context.Context, id uuid.UUID) (CampaignRecord, error) {
	page, err := u.Page(ctx)
	if err != nil {
		return CampaignRecord{}, err
	}
	for _, rec := range page.Campaigns {
		if rec.Campaign.ID == id {
			return rec, nil
		}
	}
	return CampaignRecord{}, fmt.Errorf("campaign: %w", port.ErrNotFound)
}

// Status is what the page polls: whether an after-action is being written,
// and the last failure if one is still to be seen.
func (u *Record) Status() ReviewState { return u.reviewStatus() }

func (u *Record) reviewStatus() ReviewState {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.review
}

// reviewQuestion is the record, and only the record. The mode is told not to
// add an outcome, a chance or a commendation that is not listed here.
func reviewQuestion(rec CampaignRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Write the after-action for %s (%s). Use only this record. Do not add an outcome, a chance, or a commendation that is not listed.\n",
		rec.Campaign.Name, domain.QuarterLabel(rec.Campaign.Quarter))
	writeObjectives(&b, rec.Objectives)
	writeCommendations(&b, rec.Commendations)
	writePredictions(&b, rec.Predictions)
	writeCalibration(&b, rec.Calibration)
	if rec.Campaign.Note != "" {
		fmt.Fprintf(&b, "\nMy note: %s\n", rec.Campaign.Note)
	}
	return b.String()
}

func writeObjectives(b *strings.Builder, objectives []domain.Item) {
	b.WriteString("\nObjectives assigned:\n")
	if len(objectives) == 0 {
		b.WriteString("- none\n")
		return
	}
	for _, it := range objectives {
		fmt.Fprintf(b, "- %s (%s)\n", it.Text, it.Status)
	}
}

func writeCommendations(b *strings.Builder, lines []domain.Commendation) {
	b.WriteString("\nCommendations:\n")
	if len(lines) == 0 {
		b.WriteString("- none\n")
		return
	}
	for _, line := range lines {
		fmt.Fprintf(b, "- %s: %s\n", line.Kind, line.Text)
	}
}

func writePredictions(b *strings.Builder, predictions []domain.Prediction) {
	b.WriteString("\nPredictions:\n")
	if len(predictions) == 0 {
		b.WriteString("- none\n")
		return
	}
	for _, p := range predictions {
		fmt.Fprintf(b, "- %s", p.Text)
		if p.Chance != domain.ChanceNone {
			fmt.Fprintf(b, "; chance %s", p.Chance)
		}
		if p.Outcome != domain.OutcomeNone {
			fmt.Fprintf(b, "; outcome %s", p.Outcome)
		} else if p.Status == domain.PredictionKept {
			b.WriteString("; not scored")
		}
		if p.Warning != "" {
			fmt.Fprintf(b, "; warning: %s", p.Warning)
		}
		b.WriteString("\n")
	}
}

func writeCalibration(b *strings.Builder, cal domain.Calibration) {
	b.WriteString("\nCalibration: ")
	if cal.Score == nil {
		fmt.Fprintf(b, "withheld, %d scored (a number needs %d).\n", cal.N, domain.CalibrationMin)
	} else {
		fmt.Fprintf(b, "%.2f over %d scored predictions. 0.25 is a coin flip.\n", *cal.Score, cal.N)
	}
	for _, bucket := range cal.Buckets {
		fmt.Fprintf(b, "- %s: showed %d of %d\n", bucket.Chance, bucket.Called, bucket.Scored)
	}
}
