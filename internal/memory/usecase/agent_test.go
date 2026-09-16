package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

type stubToolLLM struct {
	turns []port.ChatTurn
	calls int
	chat  string
}

func (s *stubToolLLM) Chat(_ context.Context, _ []port.Message) (string, error) {
	if s.chat != "" {
		return s.chat, nil
	}
	return "fallback answer", nil
}

func (s *stubToolLLM) ChatTools(_ context.Context, _ []port.Message, tools []port.Tool) (port.ChatTurn, error) {
	if s.calls >= len(s.turns) {
		return port.ChatTurn{Content: "done"}, nil
	}
	turn := s.turns[s.calls]
	s.calls++
	if len(tools) == 0 && turn.Content == "" && len(turn.ToolCalls) == 0 {
		return port.ChatTurn{Content: "forced answer"}, nil
	}
	return turn, nil
}

type stubInitialContext struct {
	content string
}

func (s stubInitialContext) Read() (string, error) {
	return s.content, nil
}

type stubRepo struct{}

func (stubRepo) Save(context.Context, domain.MemoryEntry) error { return nil }
func (stubRepo) SearchSimilar(context.Context, []float32, int, port.SearchFilter) ([]domain.MemoryEntry, error) {
	return []domain.MemoryEntry{
		{
			ID:    uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			Kind:  domain.KindNote,
			Title: "bench",
			Body:  "bench press 60kg",
			Score: 0.9,
		},
	}, nil
}
func (stubRepo) SearchFullText(context.Context, port.TextQuery, int, port.SearchFilter) ([]domain.MemoryEntry, error) {
	return nil, nil
}
func (stubRepo) ListRecent(context.Context, int, port.SearchFilter) ([]domain.MemoryEntry, error) {
	return nil, nil
}
func (stubRepo) DeleteBySourcePath(context.Context, string) error { return nil }
func (stubRepo) SourceContentHash(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func TestAgentConsultAnswersFromPrefetchWithoutTools(t *testing.T) {
	t.Parallel()

	llm := &stubToolLLM{
		turns: []port.ChatTurn{
			{Content: "You benched 60kg recently."},
		},
	}

	prefetch := []domain.MemoryEntry{{
		ID:    uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Kind:  domain.KindNote,
		Title: "bench",
		Body:  "bench press 60kg",
		Score: 0.9,
	}}

	retriever := &Retrieve{repo: stubRepo{}, embedder: stubEmbedder{}, candidateLimit: 8, defaultTopK: 8}
	agent := NewAgentConsult(llm, NewMemoryToolExecutor(retriever, stubRepo{}), stubInitialContext{content: "I lift"})

	result, err := agent.Execute(context.Background(), testPlan("How is my lifting?", 1), prefetch)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Answer != "You benched 60kg recently." {
		t.Fatalf("answer = %q", result.Answer)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result.Sources))
	}
	if llm.calls != 1 {
		t.Fatalf("expected 1 llm call, got %d", llm.calls)
	}
}

func TestAgentConsultUsesRecallToolOnce(t *testing.T) {
	t.Parallel()

	llm := &stubToolLLM{
		turns: []port.ChatTurn{
			{
				ToolCalls: []port.ToolCall{
					{Name: "recall_memories", Arguments: map[string]any{"query": "bench press"}},
				},
			},
			{Content: "You benched 60kg recently."},
		},
	}

	retriever := &Retrieve{repo: stubRepo{}, embedder: stubEmbedder{}, candidateLimit: 8, defaultTopK: 8}
	agent := NewAgentConsult(llm, NewMemoryToolExecutor(retriever, stubRepo{}), stubInitialContext{content: "I lift"})

	result, err := agent.Execute(context.Background(), testPlan("How is my lifting?", 1), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Answer != "You benched 60kg recently." {
		t.Fatalf("answer = %q", result.Answer)
	}
	if len(result.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(result.Sources))
	}
	if llm.calls != 2 {
		t.Fatalf("expected 2 llm calls, got %d", llm.calls)
	}
}

type stubEmbedder struct{}

func (stubEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0}, nil
}

// testPlan builds a plan with a fixed clock so recency scoring is deterministic.
func testPlan(question string, toolRounds int) ConsultPlan {
	policy := domain.PolicyFor(domain.TierStandard)
	policy.MaxToolRounds = toolRounds
	return ConsultPlan{
		Question: question,
		Tier:     policy,
		Now:      time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}
}
