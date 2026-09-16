package usecase

import (
	genfs "github.com/an4eetos/decision-room/internal/generals/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/generals/assets"
	genport "github.com/an4eetos/decision-room/internal/generals/port"
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

func TestResolvePlanUsesDefaultTier(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(testRegistry(t), domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{Question: "  what now?  "})

	if plan.Tier.Tier != domain.TierStandard {
		t.Fatalf("tier = %q, want standard", plan.Tier.Tier)
	}
	if plan.Question != "what now?" {
		t.Fatalf("question not trimmed: %q", plan.Question)
	}
	if plan.Now.IsZero() {
		t.Fatal("plan clock was not set")
	}
}

// The ceiling is a deployment limit, so a request must not be able to exceed it.
func TestResolvePlanCapsAtMaxTier(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(testRegistry(t), domain.TierStandard, domain.TierQuick).
		Resolve(ConsultInput{Question: "q", Tier: "deep"})

	if plan.Tier.Tier != domain.TierQuick {
		t.Fatalf("tier = %q, want it capped to quick", plan.Tier.Tier)
	}
	if plan.Tier.UsesTools() {
		t.Fatal("a capped-to-quick plan must not use tools")
	}
}

func TestResolvePlanExplicitTopKWins(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(testRegistry(t), domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{Question: "q", Tier: "quick", TopK: 25})

	if plan.Tier.TopK != 25 {
		t.Fatalf("topK = %d, want the caller's 25", plan.Tier.TopK)
	}
	// The rest of the tier still applies.
	if plan.Tier.MaxToolRounds != 0 {
		t.Fatal("an explicit topK should not change the tier's other knobs")
	}
}

// A quick answer gets one lens, a deep one gets three: more lenses means a
// longer structured answer, which is exactly what depth is buying.
func TestLensCountRisesWithTier(t *testing.T) {
	t.Parallel()

	resolver := NewPlanResolver(testRegistry(t), domain.TierStandard, domain.TierDeep)

	quick := resolver.Resolve(ConsultInput{Question: "should I ship this?", Tier: "quick"})
	deep := resolver.Resolve(ConsultInput{Question: "should I ship this?", Tier: "deep"})

	if len(quick.Generals) != 1 {
		t.Fatalf("quick got %d lenses, want 1", len(quick.Generals))
	}
	if len(deep.Generals) < 2 {
		t.Fatalf("deep got %d lenses, want at least 2", len(deep.Generals))
	}
}

func TestExplicitGeneralsAreHonoured(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(testRegistry(t), domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{Question: "anything", Tier: "deep", GeneralIDs: []string{"kutuzov", "patton"}})

	if plan.GeneralsMethod != "explicit" {
		t.Fatalf("method = %q, want explicit", plan.GeneralsMethod)
	}
	if got := plan.GeneralIDs(); len(got) != 2 || got[0] != "kutuzov" {
		t.Fatalf("explicit pick not used: %v", got)
	}
}

// Without a registry the app must still answer, just without lenses.
func TestNilRegistryDegradesGracefully(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(nil, domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{Question: "anything"})

	if len(plan.Generals) != 0 {
		t.Fatal("expected no lenses without a registry")
	}
	if plan.Question != "anything" {
		t.Fatal("the rest of the plan should still resolve")
	}
}

func testRegistry(t *testing.T) genport.Registry {
	t.Helper()
	roster, err := genfs.Load(assets.Generals(), assets.Styles(), "")
	if err != nil {
		t.Fatalf("load roster: %v", err)
	}
	return genfs.NewRegistry(roster)
}
