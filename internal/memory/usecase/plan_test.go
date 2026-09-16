package usecase

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

func TestResolvePlanUsesDefaultTier(t *testing.T) {
	t.Parallel()

	plan := ResolvePlan(ConsultInput{Question: "  what now?  "}, domain.TierStandard, domain.TierDeep)

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

	plan := ResolvePlan(ConsultInput{Question: "q", Tier: "deep"}, domain.TierStandard, domain.TierQuick)

	if plan.Tier.Tier != domain.TierQuick {
		t.Fatalf("tier = %q, want it capped to quick", plan.Tier.Tier)
	}
	if plan.Tier.UsesTools() {
		t.Fatal("a capped-to-quick plan must not use tools")
	}
}

func TestResolvePlanExplicitTopKWins(t *testing.T) {
	t.Parallel()

	plan := ResolvePlan(ConsultInput{Question: "q", Tier: "quick", TopK: 25}, domain.TierStandard, domain.TierDeep)

	if plan.Tier.TopK != 25 {
		t.Fatalf("topK = %d, want the caller's 25", plan.Tier.TopK)
	}
	// The rest of the tier still applies.
	if plan.Tier.MaxToolRounds != 0 {
		t.Fatal("an explicit topK should not change the tier's other knobs")
	}
}
