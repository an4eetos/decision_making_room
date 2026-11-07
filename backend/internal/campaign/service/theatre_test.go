package service

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestLayoutLineMovesAsLoopsClose(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	closed := domain.Front{ID: uuid.New(), Name: "Old", Status: domain.FrontWithdrawn}

	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive}
	fortress := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &work.ID, ObjectiveID: &ship.ID,
		Text: "No tests", Status: domain.StatusActive, Strength: 3, StrengthConfirmed: true, Kind: domain.KindTooBig}
	fog := domain.Item{ID: uuid.New(), Type: domain.TypeUnknown, FrontID: &work.ID, ObjectiveID: &ship.ID, Text: "Who pays?", Status: domain.StatusActive}
	proposal := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Maybe", Status: domain.StatusProposed}
	stray := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &closed.ID, Text: "On a closed front", Status: domain.StatusActive}

	order := comdomain.Commitment{ID: uuid.New(), Text: "Write the tests", Status: comdomain.StatusOpen, TargetID: &fortress.ID}
	recon := comdomain.Commitment{ID: uuid.New(), Text: "Ask", Status: comdomain.StatusOpen, Kind: comdomain.KindRecon, TargetID: &fog.ID}
	proposed := comdomain.Commitment{ID: uuid.New(), Text: "Not agreed", Status: comdomain.StatusProposed, TargetID: &ship.ID}

	fronts := []domain.Front{work, closed}
	items := []domain.Item{ship, fortress, fog, proposal, stray}
	orders := []comdomain.Commitment{order, recon, proposed}

	before := Layout(fronts, items, orders, now)
	if len(before.Fronts) != 2 {
		t.Fatalf("want Work and the unassigned lane, got %d lanes", len(before.Fronts))
	}
	lane := before.Fronts[0]
	if lane.Name != "Work" || len(lane.Rows) != 1 {
		t.Fatalf("Work should have one row (the proposal stays off), got %+v", lane)
	}
	row := lane.Rows[0]
	if row.Objective == nil || row.Objective.Text != "Ship v2" || len(row.Obstacles) != 1 || len(row.Unknowns) != 1 {
		t.Fatalf("row not laid out: %+v", row)
	}
	if row.Orders.Open != 2 || row.Orders.Recon != 1 || row.Orders.Done != 0 {
		t.Fatalf("orders miscounted (proposed ones should not count): %+v", row.Orders)
	}
	if lane.Line != 0 || row.Progress != 0 {
		t.Fatalf("nothing has moved yet, line %v", lane.Line)
	}
	if before.Fronts[1].ID != uuid.Nil || before.Fronts[1].Rows[0].Objective != nil || len(before.Fronts[1].Rows[0].Obstacles) != 1 {
		t.Fatalf("an item on a withdrawn front belongs in the unassigned lane: %+v", before.Fronts[1])
	}
	if before.Totals.Objectives != 1 || before.Totals.Opposition != 2 || before.Totals.Fog != 1 {
		t.Fatalf("totals: %+v", before.Totals)
	}

	// Close the loop on the fortress and lift the fog: 3 + 1 + 1 (the order)
	// of 2 + 3 + 1 + 1 + 1 held.
	order.Status = comdomain.StatusDone
	fortress.Status = domain.StatusResolved
	fog.Status = domain.StatusResolved
	after := Layout(fronts, []domain.Item{ship, fortress, fog, proposal, stray}, []comdomain.Commitment{order, recon, proposed}, now)
	if got := after.Fronts[0].Line; !near(got, 5.0/8.0) {
		t.Fatalf("line should have advanced to 5/8, got %v", got)
	}
	if after.Totals.Cleared != 1 || after.Totals.Lifted != 1 || after.Totals.OrdersDone != 1 {
		t.Fatalf("totals after: %+v", after.Totals)
	}

	// Take the objective: everything held.
	ship.Status = domain.StatusResolved
	recon.Status = comdomain.StatusDone
	taken := Layout(fronts, []domain.Item{ship, fortress, fog}, []comdomain.Commitment{order, recon}, now)
	if got := taken.Fronts[0].Line; !near(got, 1) {
		t.Fatalf("objective taken should hold all ground, got %v", got)
	}
}

func TestLayoutWithdrawnObjectiveCountsForNothing(t *testing.T) {
	t.Parallel()
	now := time.Now()
	f := domain.Front{ID: uuid.New(), Name: "Money", Status: domain.FrontActive}
	kept := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &f.ID, Text: "Keep", Status: domain.StatusResolved}
	gone := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &f.ID, Text: "Gone", Status: domain.StatusWithdrawn}
	wall := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &f.ID, ObjectiveID: &gone.ID, Text: "Wall", Status: domain.StatusActive, Strength: 3}
	loose := domain.Item{ID: uuid.New(), Type: domain.TypeUnknown, FrontID: &f.ID, Text: "Loose fog", Status: domain.StatusActive}

	th := Layout([]domain.Front{f}, []domain.Item{gone, wall, kept, loose}, nil, now)
	lane := th.Fronts[0]
	if len(lane.Rows) != 3 {
		t.Fatalf("want taken, withdrawn and a loose row, got %d", len(lane.Rows))
	}
	if lane.Rows[0].Objective.Text != "Keep" || lane.Rows[1].Objective.Text != "Gone" || lane.Rows[2].Objective != nil {
		t.Fatalf("rows should run live, taken, withdrawn, then loose: %+v", lane.Rows)
	}
	if lane.Rows[1].Progress != 0 || len(lane.Rows[1].Obstacles) != 1 {
		t.Fatalf("a withdrawn row is drawn but holds nothing: %+v", lane.Rows[1])
	}
	// Taken objective (2 of 2) plus loose fog (0 of 1); the withdrawn row
	// and its wall are out of the sum.
	if !near(lane.Line, 2.0/3.0) {
		t.Fatalf("line %v, want 2/3", lane.Line)
	}
	if th.Totals.Withdrawn != 1 || th.Totals.Taken != 1 || th.Totals.Opposition != 1 {
		t.Fatalf("totals: %+v", th.Totals)
	}
	if !lane.Rows[1].Obstacles[0].Estimated || lane.Rows[1].Obstacles[0].Strength != 3 {
		t.Fatalf("an unconfirmed strength is an estimate: %+v", lane.Rows[1].Obstacles[0])
	}
}

func TestLayoutIsDeterministicAndEmptySafe(t *testing.T) {
	t.Parallel()
	now := time.Now()
	empty := Layout(nil, nil, nil, now)
	if empty.Fronts == nil || len(empty.Fronts) != 0 {
		t.Fatalf("an empty theatre has an empty, non-nil lane list: %+v", empty.Fronts)
	}

	f := domain.Front{ID: uuid.New(), Name: "Health", Status: domain.FrontActive}
	a := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &f.ID, Text: "A", Status: domain.StatusActive}
	b := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &f.ID, ObjectiveID: &a.ID, Text: "B", Status: domain.StatusActive}
	one, _ := json.Marshal(Layout([]domain.Front{f}, []domain.Item{a, b}, nil, now))
	two, _ := json.Marshal(Layout([]domain.Front{f}, []domain.Item{a, b}, nil, now))
	if string(one) != string(two) {
		t.Fatal("the same inputs must draw the same map")
	}
}
