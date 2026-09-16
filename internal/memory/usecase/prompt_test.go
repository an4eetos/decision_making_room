package usecase

import (
	"strings"
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

func TestLastUserMessageSkipsSummary(t *testing.T) {
	t.Parallel()

	history := []port.Message{
		{Role: "user", Content: "Earlier conversation summary:\nold stuff"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "follow up"},
	}

	if got := lastUserMessage(history); got != "follow up" {
		t.Fatalf("lastUserMessage = %q, want follow up", got)
	}
}

func TestRetrievalQueryIncludesLastUserTurn(t *testing.T) {
	t.Parallel()

	query := retrievalQuery(ConsultPlan{
		Question: "what about last week?",
		History: []port.Message{
			{Role: "user", Content: "bench press"},
		},
	})

	if !strings.Contains(query, "bench press") {
		t.Fatalf("expected prior user turn in query, got %q", query)
	}
}
