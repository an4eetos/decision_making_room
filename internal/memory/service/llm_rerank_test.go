package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

func candidates(n int) []ScoredCandidate {
	out := make([]ScoredCandidate, n)
	for i := range out {
		out[i] = ScoredCandidate{Entry: domain.MemoryEntry{ID: uuid.New()}}
	}
	return out
}

func TestParseRerankOrderReadsNumbers(t *testing.T) {
	t.Parallel()

	got := ParseRerankOrder("3 1 2", 3)
	want := []int{2, 0, 1}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v (zero-based)", got, want)
		}
	}
}

func TestParseRerankOrderIgnoresOutOfRangeAndDuplicates(t *testing.T) {
	t.Parallel()

	got := ParseRerankOrder("2, 2, 99, 0, 1", 3)
	// 0 is out of range because the prompt numbers from one.
	want := []int{1, 0}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// A model returning five of twelve expressed a preference about five, not a
// decision to discard seven.
func TestApplyRerankOrderKeepsUnrankedCandidates(t *testing.T) {
	t.Parallel()

	in := candidates(5)
	out := ApplyRerankOrder(in, []int{4, 0})

	if len(out) != 5 {
		t.Fatalf("got %d candidates, want all 5 kept", len(out))
	}
	if out[0].Entry.ID != in[4].Entry.ID || out[1].Entry.ID != in[0].Entry.ID {
		t.Fatal("ranked candidates should come first, in the given order")
	}
}

// The rerank is optional, so an unusable response must leave retrieval exactly
// as the heuristic left it.
func TestApplyRerankOrderWithNoOrderIsIdentity(t *testing.T) {
	t.Parallel()

	in := candidates(4)
	out := ApplyRerankOrder(in, nil)

	if len(out) != len(in) {
		t.Fatalf("got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i].Entry.ID != in[i].Entry.ID {
			t.Fatal("order changed when there was no ranking to apply")
		}
	}
}

func TestParseRerankOrderHandlesProse(t *testing.T) {
	t.Parallel()

	got := ParseRerankOrder("I think note 2 is most useful, then 1.", 3)
	if len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("got %v, want [1 0]", got)
	}
}
