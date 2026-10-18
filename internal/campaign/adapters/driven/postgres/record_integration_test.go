package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

func TestCampaignRecordRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	opened, err := repo.CreateCampaign(ctx, domain.Campaign{
		Quarter: "2026-Q4", Name: "2026 Q4", Phase: domain.PhasePlanning, OpenedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.CreateCampaign(ctx, domain.Campaign{
		Quarter: "2026-Q4", Name: "again", Phase: domain.PhasePlanning, OpenedAt: time.Now(),
	}); !errors.Is(err, port.ErrDuplicate) {
		t.Fatalf("a quarter is unique: %v", err)
	}
	if _, err := repo.CreateCampaign(ctx, domain.Campaign{
		Quarter: "2026-Q5", Name: "nope", Phase: domain.PhasePlanning, OpenedAt: time.Now(),
	}); err == nil {
		t.Fatal("a quarter key is 2026-Q4, not Q5")
	}

	objective, err := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObjective, Text: "Ship the beta", Status: domain.StatusActive,
		Source: domain.SourceManual, Confidence: 1, Fingerprint: "beta-record",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignObjective(ctx, opened.ID, objective.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AssignObjective(ctx, opened.ID, objective.ID); !errors.Is(err, port.ErrDuplicate) {
		t.Fatalf("assigning twice: %v", err)
	}

	pred, err := repo.CreatePrediction(ctx, domain.Prediction{
		CampaignID: &opened.ID, ObjectiveID: &objective.ID, Text: "The beta shipped without tests",
		Status: domain.PredictionProposed, Source: "pre_mortem", Fingerprint: "no-tests",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreatePrediction(ctx, domain.Prediction{
		Text: "The beta shipped without tests", Status: domain.PredictionProposed,
		Source: "pre_mortem", Fingerprint: "no-tests",
	}); !errors.Is(err, port.ErrDuplicate) {
		t.Fatalf("a live prediction is unique: %v", err)
	}
	pred.Chance = domain.ChanceLikely
	if _, err := repo.UpdatePrediction(ctx, pred); err == nil {
		t.Fatal("a chance belongs to a kept prediction")
	}

	pred.Status = domain.PredictionKept
	now := time.Now()
	pred.Outcome, pred.ScoredAt = domain.OutcomeCalled, &now
	kept, err := repo.UpdatePrediction(ctx, pred)
	if err != nil || kept.Chance != domain.ChanceLikely || kept.Outcome != domain.OutcomeCalled {
		t.Fatalf("score: %+v %v", kept, err)
	}

	score := 0.04
	opened.ClosedAt, opened.Calibration, opened.CalibrationN = &now, &score, 5
	sealed, err := repo.SealCampaign(ctx, opened, []domain.Commendation{{
		Kind: domain.CommendationCalled, Text: pred.Text, RefID: &pred.ID, CreatedAt: now,
	}})
	if err != nil || sealed.Phase != domain.PhaseClosed || sealed.Calibration == nil || *sealed.Calibration != score {
		t.Fatalf("seal: %+v %v", sealed, err)
	}
	lines, err := repo.ListCommendations(ctx, opened.ID)
	if err != nil || len(lines) != 1 || lines[0].Kind != domain.CommendationCalled {
		t.Fatalf("sealed lines: %+v %v", lines, err)
	}
	if _, err := repo.UpdateCampaign(ctx, sealed); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("a closed quarter is not rewritten: %v", err)
	}
	if err := repo.DeleteCampaign(ctx, opened.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("a closed quarter is not deleted: %v", err)
	}
}
