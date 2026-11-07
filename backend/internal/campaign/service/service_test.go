package service

import (
	"slices"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

func TestPrefilterLetsIntelThroughAndStopsChatter(t *testing.T) {
	t.Parallel()

	through := []string{
		"I want to move to Lisbon by March.",
		"I'm stuck on the visa paperwork.",
		"Not sure whether the landlord will accept a foreign income.",
		"I’m waiting on the bank to send the statement.",
		"My goal is to ship the beta this quarter.",
		"Honestly I'm afraid the numbers will be bad.",
	}
	for _, text := range through {
		if !LooksLikeIntel(text) {
			t.Errorf("%q should reach extraction", text)
		}
	}

	stopped := []string{
		"Plan my day.",
		"Thanks, that helps.",
		"What did I decide about the database?",
		"Summarise this week.",
	}
	for _, text := range stopped {
		if LooksLikeIntel(text) {
			t.Errorf("%q should not cost an extraction call", text)
		}
	}
}

func TestParseCandidates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

	answer := "```json\n" + `{
	  "objectives": [
	    {"text": "Move to Lisbon", "front": "Life", "due": "2027-03-01", "confidence": 0.9},
	    {"text": "Too unsure", "confidence": 0.3},
	    {"text": "Second", "confidence": 0.8},
	    {"text": "Third is over the cap", "confidence": 0.8}
	  ],
	  "obstacles": [
	    {"text": "Bank statement not sent", "objective": 1, "kind": "waiting", "strength": 1, "confidence": 0.8},
	    {"text": "Landlord might refuse", "objective": "Move to Lisbon", "kind": "nonsense", "strength": 9, "confidence": 0.7}
	  ],
	  "unknowns": [
	    {"text": "Will the landlord accept foreign income?", "objective": "2", "confidence": 0.75}
	  ]
	}` + "\n```"

	got, err := ParseCandidates(answer, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d candidates, want 5 (2 objectives, 2 obstacles, 1 unknown): %+v", len(got), got)
	}

	lisbon := got[0]
	if lisbon.Type != domain.TypeObjective || lisbon.Front != "Life" || lisbon.Due == nil {
		t.Fatalf("objective parsed wrong: %+v", lisbon)
	}

	bank, landlord, question := got[2], got[3], got[4]
	if bank.Kind != domain.KindWaiting || bank.Strength != 1 || bank.ObjectiveIndex != 1 {
		t.Fatalf("obstacle parsed wrong: %+v", bank)
	}
	// An unknown kind is no kind; a strength off the scale becomes the middle,
	// to be confirmed or corrected.
	if landlord.Kind != "" || landlord.Strength != domain.StrengthDugIn || landlord.ObjectiveText != "Move to Lisbon" {
		t.Fatalf("obstacle with bad fields parsed wrong: %+v", landlord)
	}
	if question.Type != domain.TypeUnknown || question.ObjectiveIndex != 2 {
		t.Fatalf("unknown parsed wrong: %+v", question)
	}
}

func TestParseCandidatesEmptyAndBroken(t *testing.T) {
	t.Parallel()
	now := time.Now()

	if got, err := ParseCandidates("", now); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	if got, err := ParseCandidates(`{"objectives": [], "obstacles": [], "unknowns": []}`, now); err != nil || len(got) != 0 {
		t.Fatalf("nothing to map: %v %v", got, err)
	}
	if _, err := ParseCandidates("no json here", now); err == nil {
		t.Fatal("prose without JSON should be an error")
	}
	// A due date already past is a misparse, not an overdue objective.
	got, _ := ParseCandidates(`{"objectives": [{"text": "x", "due": "2001-01-01", "confidence": 1}]}`, now)
	if len(got) != 1 || got[0].Due != nil {
		t.Fatalf("past due should be dropped: %+v", got)
	}
}

func TestParseInterrogation(t *testing.T) {
	t.Parallel()

	turn := `**On the record**
- Cash: $14,000.

**Still dark**
- Housing or legal right to reside in Portugal.
- What date is the mother told?

**Caught**
- Vague intent: "I would want to move"

**Questions**
**Eisenhower:** On what date do you tell your mother?
**Zhukov:** What is the exact cash reserve?
**Probe:** Check the price of a one-way ticket to Lisbon and the residency rules before Sunday.`

	intel := ParseInterrogation(turn)
	want := []string{"Housing or legal right to reside in Portugal.", "What date is the mother told?"}
	if !slices.Equal(intel.Unknowns, want) {
		t.Fatalf("unknowns = %q, want %q", intel.Unknowns, want)
	}
	if intel.Probe != "Check the price of a one-way ticket to Lisbon and the residency rules before Sunday." {
		t.Fatalf("probe = %q", intel.Probe)
	}
	if len(intel.Orders) != 0 {
		t.Fatalf("a questioning turn has no orders, got %q", intel.Orders)
	}
}

func TestParseInterrogationReadsThePosition(t *testing.T) {
	t.Parallel()

	position := `**Position**
Stay until November, then go.

**Orders**
- **Zhukov:** Submit your resignation by 17:00 on November 15.
- **Raid:** Draft the email tomorrow.

**Accepted dark**
- Post-exit income source. (Reviewed November 15.)`

	intel := ParseInterrogation(position)
	if !slices.Equal(intel.Orders, []string{"Submit your resignation by 17:00 on November 15.", "Draft the email tomorrow."}) {
		t.Fatalf("orders = %q", intel.Orders)
	}
	if len(intel.Unknowns) != 1 || intel.Probe != "" {
		t.Fatalf("unknowns = %q probe = %q", intel.Unknowns, intel.Probe)
	}

	ready := ParseInterrogation("**Still dark**\nReady to take a position.\n\n**Questions**\n**Zhukov:** q")
	if len(ready.Unknowns) != 0 {
		t.Fatalf("the readiness line is not an unknown: %q", ready.Unknowns)
	}
	if got := ParseInterrogation("Plain prose with no sections."); len(got.Unknowns)+len(got.Orders) != 0 || got.Probe != "" {
		t.Fatalf("prose should yield nothing: %+v", got)
	}
}

func TestOverlapLinksAProbeToItsUnknown(t *testing.T) {
	t.Parallel()

	probe := "Check the residency rules for Portugal before Sunday."
	if Overlap(probe, "Legal right to reside in Portugal under residency rules?") < 2 {
		t.Fatal("a probe and the unknown it scouts should overlap")
	}
	if Overlap(probe, "What is the exact cash reserve?") != 0 {
		t.Fatal("unrelated texts should not overlap")
	}
}
