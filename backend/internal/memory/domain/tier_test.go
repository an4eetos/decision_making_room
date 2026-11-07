package domain

import "testing"

func TestPolicyForCostsRiseWithTier(t *testing.T) {
	t.Parallel()

	quick, standard, deep := PolicyFor(TierQuick), PolicyFor(TierStandard), PolicyFor(TierDeep)

	if !(quick.TopK < standard.TopK && standard.TopK < deep.TopK) {
		t.Fatalf("topK should rise with tier: %d %d %d", quick.TopK, standard.TopK, deep.TopK)
	}
	if !(quick.CandidateLimit < standard.CandidateLimit && standard.CandidateLimit < deep.CandidateLimit) {
		t.Fatal("candidate limit should rise with tier")
	}
	if !(quick.MaxToolRounds < standard.MaxToolRounds && standard.MaxToolRounds < deep.MaxToolRounds) {
		t.Fatal("tool rounds should rise with tier")
	}
}

// Quick must not enter the agent loop at all; that is what makes it quick.
func TestQuickUsesNoTools(t *testing.T) {
	t.Parallel()

	if PolicyFor(TierQuick).UsesTools() {
		t.Fatal("quick tier must not use tools")
	}
	if !PolicyFor(TierStandard).UsesTools() {
		t.Fatal("standard tier should use tools")
	}
}

func TestParseTierFallsBackRatherThanFailing(t *testing.T) {
	t.Parallel()

	for _, in := range []string{"", "  ", "turbo", "DEEPEST"} {
		if got := ParseTier(in, TierStandard); got != TierStandard {
			t.Fatalf("ParseTier(%q) = %q, want the fallback", in, got)
		}
	}
	if got := ParseTier("  DEEP  ", TierStandard); got != TierDeep {
		t.Fatalf("ParseTier should trim and lowercase, got %q", got)
	}
}

func TestCapTo(t *testing.T) {
	t.Parallel()

	if got := TierDeep.CapTo(TierQuick); got != TierQuick {
		t.Fatalf("deep capped at quick = %q", got)
	}
	if got := TierQuick.CapTo(TierDeep); got != TierQuick {
		t.Fatalf("capping must never raise a tier, got %q", got)
	}
	if got := TierStandard.CapTo(TierStandard); got != TierStandard {
		t.Fatalf("got %q", got)
	}
}
