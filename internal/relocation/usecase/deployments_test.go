package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/port"
)

// planStore is the plan repository with only the reads Deployments makes.
type planStore struct {
	port.PlanRepository
	plans []domain.Plan
}

func (s planStore) ListPlans(context.Context) ([]domain.Plan, error) {
	out := make([]domain.Plan, 0, len(s.plans))
	for _, p := range s.plans {
		p.Items, p.Pitfalls = nil, nil // a summary, like the real list
		out = append(out, p)
	}
	return out, nil
}

func (s planStore) GetPlan(_ context.Context, id uuid.UUID) (domain.Plan, error) {
	for _, p := range s.plans {
		if p.ID == id {
			return p, nil
		}
	}
	return domain.Plan{}, port.ErrNotFound
}

func TestDeploymentsCarryThePlannersOwnBudget(t *testing.T) {
	t.Parallel()
	front := uuid.New()
	price := func(v float64) *float64 { return &v }
	acknowledged := time.Now()

	lisbon := domain.Plan{
		ID: uuid.New(), Destination: "Lisbon", Status: domain.PlanActive, FrontID: &front, Currency: "EUR",
		ArriveOn: time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), DepartOn: time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC),
		Items: []domain.Item{
			{Name: "Sheets", Quantity: 2, UnitCost: price(30), Currency: "EUR", Source: domain.CostFromModel, Status: domain.ItemNeeded},
			{Name: "Knife", Quantity: 1, UnitCost: price(40), Currency: "EUR", Source: domain.CostFromUser, Status: domain.ItemBought},
			{Name: "Adapter", Quantity: 1, Currency: "EUR", Source: domain.CostFromCatalog, Status: domain.ItemNeeded},
			{Name: "Towels", Quantity: 2, UnitCost: price(20), Currency: "EUR", Source: domain.CostFromModel, Status: domain.ItemHave},
		},
		Pitfalls: []domain.PlanPitfall{
			{Severity: domain.SeverityCritical},
			{Severity: domain.SeverityCritical, AcknowledgedAt: &acknowledged},
			{Severity: domain.SeverityCostly},
		},
	}
	offMap := domain.Plan{ID: uuid.New(), Destination: "Porto", Status: domain.PlanActive}
	done := domain.Plan{ID: uuid.New(), Destination: "Tbilisi", Status: domain.PlanDone, FrontID: &front}
	abandoned := domain.Plan{ID: uuid.New(), Destination: "Bali", Status: domain.PlanAbandoned, FrontID: &front}

	got, err := NewDeployments(planStore{plans: []domain.Plan{lisbon, offMap, done, abandoned}}).Deployments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("only a live stay on a front is a deployment: %+v", got)
	}
	d := got[0]
	if d.PlanID != lisbon.ID || d.FrontID != front || d.Destination != "Lisbon" || d.ArriveOn != "2026-11-03" || d.DepartOn != "2027-01-03" {
		t.Fatalf("deployment: %+v", d)
	}
	// 60 estimated + 40 confirmed; the towels you carry cost nothing, the
	// adapter's unknown price is counted, not read as free.
	if d.Currency != "EUR" || d.Cost != 100 || d.EstimatedShare != 0.6 || d.Unpriced != 1 {
		t.Fatalf("budget: %+v", d)
	}
	if d.ItemsNeeded != 2 || d.ItemsResolved != 2 || d.CriticalOpen != 1 {
		t.Fatalf("readiness: %+v", d)
	}
}
