package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
)

type scriptedLLM struct {
	answer string
	err    error
}

func (s scriptedLLM) Chat(context.Context, []port.Message) (string, error) {
	return s.answer, s.err
}

func deepCandidates(n int) []service.ScoredCandidate {
	out := make([]service.ScoredCandidate, n)
	for i := range out {
		out[i] = service.ScoredCandidate{
			Entry: domain.MemoryEntry{ID: uuid.New(), Title: "note", Body: "body"},
		}
	}
	return out
}

func TestSubQueriesReturnsParsedQueries(t *testing.T) {
	t.Parallel()

	got := NewDeep(scriptedLLM{answer: "berlin salary\ncost of living"}).
		SubQueries(context.Background(), "should I move to berlin")

	if len(got) != 2 {
		t.Fatalf("got %v, want two sub-queries", got)
	}
}

// Decomposition is an optional improvement, so a failure means "search the
// question as asked" — never an error and never fewer results.
func TestSubQueriesDegradesToNothing(t *testing.T) {
	t.Parallel()

	cases := map[string]scriptedLLM{
		"call failed": {err: errors.New("503 overloaded")},
		"empty":       {answer: ""},
		"only prose":  {answer: "Sure! Here are some queries:"},
	}

	for name, llm := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := NewDeep(llm).SubQueries(context.Background(), "a question"); len(got) != 0 {
				t.Fatalf("got %v, want nothing", got)
			}
		})
	}
}

func TestSubQueriesWithNoLLM(t *testing.T) {
	t.Parallel()

	if got := NewDeep(nil).SubQueries(context.Background(), "q"); got != nil {
		t.Fatalf("got %v, want nil without a model", got)
	}
	var deep *Deep
	if got := deep.SubQueries(context.Background(), "q"); got != nil {
		t.Fatalf("a nil Deep should be safe, got %v", got)
	}
}

func TestRerankReordersCandidates(t *testing.T) {
	t.Parallel()

	in := deepCandidates(3)
	out := NewDeep(scriptedLLM{answer: "3 1 2"}).Rerank(context.Background(), "q", in, 3)

	if len(out) != 3 {
		t.Fatalf("got %d candidates, want 3", len(out))
	}
	if out[0].Entry.ID != in[2].Entry.ID {
		t.Fatal("expected the model's first choice to lead")
	}
}

// The reranker must never be able to make retrieval worse than not running it.
// Every failure returns the heuristic ordering untouched.
func TestRerankDegradesToHeuristicOrder(t *testing.T) {
	t.Parallel()

	cases := map[string]scriptedLLM{
		"call failed":  {err: errors.New("429 quota")},
		"no numbers":   {answer: "I think they are all quite good really"},
		"empty":        {answer: ""},
		"out of range": {answer: "99 100 101"},
	}

	for name, llm := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := deepCandidates(4)
			out := NewDeep(llm).Rerank(context.Background(), "q", in, 4)

			if len(out) != len(in) {
				t.Fatalf("got %d candidates, want all %d", len(out), len(in))
			}
			for i := range in {
				if out[i].Entry.ID != in[i].Entry.ID {
					t.Fatalf("%s: order changed despite an unusable response", name)
				}
			}
		})
	}
}

func TestRerankSkipsTrivialLists(t *testing.T) {
	t.Parallel()

	// One candidate cannot be reordered, so no model call is worth making.
	in := deepCandidates(1)
	out := NewDeep(scriptedLLM{err: errors.New("should not be called")}).
		Rerank(context.Background(), "q", in, 1)

	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
}
