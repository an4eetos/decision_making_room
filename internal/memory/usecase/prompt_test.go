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

// The map travels as the person's own context, between about-me and what
// retrieval found, and an empty map leaves the block exactly as it was.
func TestContextBlockCarriesTheMap(t *testing.T) {
	t.Parallel()

	without := buildContextBlock("I am Anuar.", "", "[2026-10-13 | note] Title: x\nBody: y")
	if strings.Contains(without, "campaign map") {
		t.Fatalf("an empty map should add nothing:\n%s", without)
	}

	with := buildContextBlock("I am Anuar.", "Front: Work — line 40%", "[2026-10-13 | note] Title: x\nBody: y")
	about := strings.Index(with, "About me")
	mapAt := strings.Index(with, "The campaign map")
	retrieved := strings.Index(with, "Retrieved context")
	if about < 0 || mapAt < 0 || retrieved < 0 || !(about < mapAt && mapAt < retrieved) {
		t.Fatalf("want about me, then the map, then retrieval:\n%s", with)
	}
	if !strings.Contains(with, "Front: Work — line 40%") {
		t.Fatalf("the brief should be in the block:\n%s", with)
	}

	messages := buildConsultMessages("system", "about", "Front: Work", nil, "", "q")
	if len(messages) != 3 || !strings.Contains(messages[1].Content, "The campaign map") || messages[1].Role != "user" {
		t.Fatalf("the map rides in the user context message: %+v", messages)
	}
}
