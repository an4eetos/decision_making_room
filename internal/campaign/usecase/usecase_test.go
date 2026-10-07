package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

func setup() (*Manage, *memRepo, *memOrders) {
	repo, orders := newMemRepo(), newMemOrders()
	return NewManage(repo, comusecase.NewManage(orders)), repo, orders
}

func ptr[T any](v T) *T { return &v }

func TestFrontsAndStarters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _, _ := setup()

	if _, err := m.CreateFront(ctx, "  "); err == nil {
		t.Fatal("a front needs a name")
	}
	work, err := m.CreateFront(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	// Starters skip what exists, so pressing it twice changes nothing.
	fronts, err := m.AddStarters(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fronts) != len(domain.StarterFronts) {
		t.Fatalf("got %d fronts, want %d", len(fronts), len(domain.StarterFronts))
	}
	if again, _ := m.AddStarters(ctx); len(again) != len(fronts) {
		t.Fatal("starters should be idempotent")
	}

	withdrawn := domain.FrontWithdrawn
	if _, err := m.UpdateFront(ctx, work.ID, nil, &withdrawn); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "x", FrontID: &work.ID}); err == nil {
		t.Fatal("nothing should be added to a withdrawn front")
	}
}

func TestCreateItemPlacesAndValidates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _, _ := setup()
	front, _ := m.CreateFront(ctx, "Life")

	objective, err := m.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Move to Lisbon", FrontID: &front.ID})
	if err != nil {
		t.Fatal(err)
	}
	if objective.Status != domain.StatusActive || objective.Source != domain.SourceManual {
		t.Fatalf("an item you add goes straight onto the map: %+v", objective)
	}

	// Opposition under an objective inherits its front; a strength you set is
	// confirmed, not an estimate.
	obstacle, err := m.CreateItem(ctx, CreateInput{
		Type: domain.TypeObstacle, Text: "Bank statement", ObjectiveID: &objective.ID,
		Kind: domain.KindWaiting, Strength: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if obstacle.FrontID == nil || *obstacle.FrontID != front.ID || !obstacle.StrengthConfirmed {
		t.Fatalf("obstacle placed wrong: %+v", obstacle)
	}

	bad := []CreateInput{
		{Type: "plan", Text: "x"},
		{Type: domain.TypeObjective, Text: ""},
		{Type: domain.TypeObjective, Text: "under an objective", ObjectiveID: &objective.ID},
		{Type: domain.TypeObjective, Text: "objectives have no kind", Kind: domain.KindFear},
		{Type: domain.TypeObstacle, Text: "strength off the scale", Strength: 5},
		{Type: domain.TypeObstacle, Text: "bears on an obstacle", ObjectiveID: &obstacle.ID},
	}
	for _, in := range bad {
		if _, err := m.CreateItem(ctx, in); err == nil {
			t.Errorf("expected %+v to be rejected", in)
		}
	}
}

func TestProposalLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, repo, _ := setup()
	front, _ := m.CreateFront(ctx, "Work")

	proposed, _ := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObstacle, Text: "Waiting on legal", Status: domain.StatusProposed,
		Kind: domain.KindWaiting, Strength: 2, Fingerprint: "a",
	})

	// A proposal cannot be cleared before it is kept.
	if _, err := m.Update(ctx, proposed.ID, Patch{Status: ptr(domain.StatusResolved)}); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("want a bad transition, got %v", err)
	}
	// Filing it under a front keeps it.
	kept, err := m.Update(ctx, proposed.ID, Patch{FrontID: &front.ID})
	if err != nil {
		t.Fatal(err)
	}
	if kept.Status != domain.StatusActive || kept.StrengthConfirmed {
		t.Fatalf("filing should keep it, and leave the strength an estimate: %+v", kept)
	}
	confirmed, _ := m.Update(ctx, kept.ID, Patch{ConfirmStrength: true})
	if !confirmed.StrengthConfirmed || confirmed.Strength != 2 {
		t.Fatalf("confirming should keep the estimate as it stands: %+v", confirmed)
	}
	if _, err := m.Update(ctx, kept.ID, Patch{Status: ptr(domain.StatusWithdrawn)}); err == nil {
		t.Fatal("only an objective is withdrawn")
	}
	cleared, err := m.Update(ctx, kept.ID, Patch{Status: ptr(domain.StatusResolved)})
	if err != nil || cleared.ResolvedAt == nil {
		t.Fatalf("clearing should resolve it: %+v %v", cleared, err)
	}

	// Dropping a proposal in the same request as an edit drops it.
	other, _ := repo.CreateItem(ctx, domain.Item{Type: domain.TypeObjective, Text: "x", Status: domain.StatusProposed, Fingerprint: "b"})
	dropped, _ := m.Update(ctx, other.ID, Patch{FrontID: &front.ID, Status: ptr(domain.StatusDropped)})
	if dropped.Status != domain.StatusDropped {
		t.Fatalf("an explicit drop wins over the implicit keep: %+v", dropped)
	}
}

func TestLiftNeedsAnAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, _, _ := setup()

	unknown, _ := m.CreateItem(ctx, CreateInput{Type: domain.TypeUnknown, Text: "Will the landlord accept foreign income?"})
	if _, err := m.Lift(ctx, unknown.ID, "  "); err == nil {
		t.Fatal("fog lifted without an answer has only been forgotten")
	}
	lifted, err := m.Lift(ctx, unknown.ID, "Yes, with six months up front.")
	if err != nil || lifted.Status != domain.StatusResolved || lifted.Answer == "" {
		t.Fatalf("lift: %+v %v", lifted, err)
	}

	objective, _ := m.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "x"})
	if _, err := m.Lift(ctx, objective.ID, "answer"); err == nil {
		t.Fatal("only an unknown is lifted")
	}
}

func TestReconAndAim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, repo, orders := setup()

	unknown, _ := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeUnknown, Text: "Is the visa process six weeks or six months", Status: domain.StatusProposed, Fingerprint: "u",
	})
	recon, err := m.SendRecon(ctx, unknown.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recon.Kind != comdomain.KindRecon || recon.TargetID == nil || *recon.TargetID != unknown.ID || recon.Status != comdomain.StatusOpen {
		t.Fatalf("recon order wrong: %+v", recon)
	}
	if !strings.HasPrefix(recon.Text, "Find out: ") || !strings.HasSuffix(recon.Text, "?") {
		t.Fatalf("recon text = %q", recon.Text)
	}
	if got, _ := repo.GetItem(ctx, unknown.ID); got.Status != domain.StatusActive {
		t.Fatal("sending recon keeps a proposed unknown")
	}

	objective, _ := m.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Move to Lisbon"})
	loop, _ := orders.Create(ctx, comdomain.Commitment{Text: "Call the consulate", Status: comdomain.StatusOpen, Fingerprint: "c"})

	aimed, err := m.Aim(ctx, loop.ID, &objective.ID)
	if err != nil || aimed.Kind != comdomain.KindOrder || *aimed.TargetID != objective.ID {
		t.Fatalf("aim at objective: %+v %v", aimed, err)
	}
	aimed, _ = m.Aim(ctx, loop.ID, &unknown.ID)
	if aimed.Kind != comdomain.KindRecon {
		t.Fatal("an order aimed at fog is reconnaissance")
	}
	aimed, _ = m.Aim(ctx, loop.ID, nil)
	if aimed.TargetID != nil || aimed.Kind != comdomain.KindOrder {
		t.Fatalf("unaiming: %+v", aimed)
	}

	refs, _ := m.ActiveObjectives(ctx)
	if len(refs) != 1 || refs[0].ID != objective.ID {
		t.Fatalf("active objectives = %+v", refs)
	}
}

func TestMapLeavesOutDroppedAndOldResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, repo, _ := setup()

	live, _ := m.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "live"})
	repo.CreateItem(ctx, domain.Item{Type: domain.TypeObjective, Text: "dropped", Status: domain.StatusDropped, Fingerprint: "d"})
	old := time.Now().Add(-60 * 24 * time.Hour)
	repo.CreateItem(ctx, domain.Item{Type: domain.TypeObjective, Text: "old", Status: domain.StatusResolved, Fingerprint: "o", ResolvedAt: &old})
	recent := time.Now().Add(-2 * 24 * time.Hour)
	repo.CreateItem(ctx, domain.Item{Type: domain.TypeObjective, Text: "recent", Status: domain.StatusResolved, Fingerprint: "r", ResolvedAt: &recent})

	got, err := m.Map(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, it := range got.Items {
		texts = append(texts, it.Text)
	}
	if strings.Join(texts, ",") != "live,recent" {
		t.Fatalf("map items = %v", texts)
	}
	_ = live
}

func TestExtractFilesUnderFrontsAndLinksObjectives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	m, repo, orders := setup()
	life, _ := m.CreateFront(ctx, "Life")
	existing, _ := m.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Get fit", FrontID: &life.ID})

	llm := &fakeLLM{answer: `{
	  "objectives": [{"text": "Move to Lisbon", "front": "life", "confidence": 0.9},
	                 {"text": "Get fit", "front": "Life", "confidence": 0.9}],
	  "obstacles": [{"text": "Knee injury", "objective": 1, "kind": "fear", "strength": 3, "confidence": 0.8},
	                {"text": "No visa yet", "front": "Relocation", "objective": "Move to Lisbon", "kind": "waiting", "strength": 2, "confidence": 0.8}],
	  "unknowns": [{"text": "How long does the D7 visa take?", "confidence": 0.7}]
	}`}
	extract := NewExtract(llm, repo, comusecase.NewManage(orders), true)

	turn := memport.Turn{SessionID: uuid.New(), MessageID: uuid.New(), UserText: "I want to move to Lisbon but no visa yet"}
	created, err := extract.Run(ctx, turn)
	if err != nil {
		t.Fatal(err)
	}
	// "Get fit" is already on the map; the dedupe skips it.
	if len(created) != 4 {
		t.Fatalf("created %d items, want 4", len(created))
	}

	lisbon := repo.byText("Move to Lisbon")
	if lisbon.Status != domain.StatusProposed || lisbon.FrontID == nil || *lisbon.FrontID != life.ID {
		t.Fatalf("a front name matches case-insensitively, and extraction only proposes: %+v", lisbon)
	}
	knee := repo.byText("Knee injury")
	if knee.ObjectiveID == nil || *knee.ObjectiveID != existing.ID || knee.FrontID == nil || *knee.FrontID != life.ID {
		t.Fatalf("an obstacle linked by number sits under that objective and its front: %+v", knee)
	}
	if knee.StrengthConfirmed {
		t.Fatal("an extracted strength is an estimate")
	}
	visa := repo.byText("No visa yet")
	if visa.ObjectiveID == nil || *visa.ObjectiveID != lisbon.ID {
		t.Fatal("an obstacle can point at an objective proposed in the same answer")
	}
	if visa.FrontID != nil {
		t.Fatal("extraction never invents a front: an unknown name is no front")
	}

	prompt := llm.got[0].Content
	if !strings.Contains(prompt, "Their fronts (areas of life and work): Life.") || !strings.Contains(prompt, "1. Get fit") {
		t.Fatalf("the prompt should carry fronts and numbered objectives:\n%s", prompt)
	}
}

func TestFromInterrogationNeedsNoModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, repo, orders := setup()
	extract := NewExtract(nil, repo, comusecase.NewManage(orders), true)

	turn := memport.Turn{
		SessionID: uuid.New(), MessageID: uuid.New(), ModeID: "interrogation",
		AssistantText: `**Still dark**
- Housing or legal right to reside in Portugal.
- Ready to take a position.

**Questions**
**Zhukov:** What is the cash reserve?
**Probe:** Check the residency rules for Portugal before Sunday.`,
	}
	if err := extract.FromInterrogation(ctx, turn); err != nil {
		t.Fatal(err)
	}

	fog := repo.byText("Housing or legal right to reside in Portugal.")
	if fog.Type != domain.TypeUnknown || fog.Status != domain.StatusProposed || fog.Source != domain.SourceInterrogation {
		t.Fatalf("still dark should become proposed fog: %+v", fog)
	}
	all := orders.all()
	if len(all) != 1 {
		t.Fatalf("want one proposed recon order, got %+v", all)
	}
	probe := all[0]
	if probe.Status != comdomain.StatusProposed || probe.Kind != comdomain.KindRecon || probe.TargetID == nil || *probe.TargetID != fog.ID {
		t.Fatalf("the probe should be a proposed recon order aimed at the fog it scouts: %+v", probe)
	}

	// The same turn again adds nothing: the dedupe holds for both.
	if err := extract.FromInterrogation(ctx, turn); err != nil {
		t.Fatal(err)
	}
	if len(orders.all()) != 1 {
		t.Fatal("a repeated probe should not be proposed twice")
	}
}

// A probe that attacks an obstacle is aimed at it as a plain order.
func TestProbeAimsAtTheObstacleItAttacks(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, repo, orders := setup()
	extract := NewExtract(nil, repo, comusecase.NewManage(orders), true)

	bank, _ := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObstacle, Text: "Bank has not sent the income statement", Status: domain.StatusActive, Fingerprint: "bank",
	})
	repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeUnknown, Text: "How long does the visa take?", Status: domain.StatusActive, Fingerprint: "visa",
	})

	turn := memport.Turn{SessionID: uuid.New(), MessageID: uuid.New(), ModeID: "interrogation",
		AssistantText: "**Questions**\n**Probe:** Call the bank and demand the dispatch date of the income statement."}
	if err := extract.FromInterrogation(ctx, turn); err != nil {
		t.Fatal(err)
	}
	all := orders.all()
	if len(all) != 1 || all[0].TargetID == nil || *all[0].TargetID != bank.ID || all[0].Kind == comdomain.KindRecon {
		t.Fatalf("the probe should be an order aimed at the bank obstacle: %+v", all)
	}
}
