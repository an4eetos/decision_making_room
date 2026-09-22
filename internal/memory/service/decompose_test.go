package service

import (
	"strings"
	"testing"
)

func TestParseSubQueriesStripsListMarkup(t *testing.T) {
	t.Parallel()

	got := ParseSubQueries("1. berlin job offer\n2) salary comparison\n- cost of living berlin", "should I move")
	want := []string{"berlin job offer", "salary comparison", "cost of living berlin"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// Searching the same thing twice wastes an arm and skews fusion toward whatever
// that arm found.
func TestParseSubQueriesDropsDuplicatesAndTheOriginal(t *testing.T) {
	t.Parallel()

	got := ParseSubQueries("Should I move\nberlin salary\nBERLIN  SALARY\n", "should I move")
	if len(got) != 1 || got[0] != "berlin salary" {
		t.Fatalf("got %v, want just the one new query", got)
	}
}

func TestParseSubQueriesRejectsCommentary(t *testing.T) {
	t.Parallel()

	answer := "Here are three queries:\nSure, I can help.\nberlin rent prices"
	got := ParseSubQueries(answer, "original")
	if len(got) != 1 || got[0] != "berlin rent prices" {
		t.Fatalf("commentary survived: %v", got)
	}
}

func TestParseSubQueriesCapsCount(t *testing.T) {
	t.Parallel()

	answer := strings.Join([]string{"one query", "two query", "three query", "four query", "five query"}, "\n")
	if got := ParseSubQueries(answer, "x"); len(got) > MaxSubQueries {
		t.Fatalf("got %d queries, capped at %d", len(got), MaxSubQueries)
	}
}

// Decomposition is an optional improvement. Garbage in must mean "search the
// question as asked", never an error.
func TestParseSubQueriesHandlesGarbage(t *testing.T) {
	t.Parallel()

	for _, answer := range []string{"", "   ", "\n\n\n", "!!!", "ok"} {
		if got := ParseSubQueries(answer, "original"); len(got) != 0 {
			t.Fatalf("answer %q produced %v; expected nothing usable", answer, got)
		}
	}
}
