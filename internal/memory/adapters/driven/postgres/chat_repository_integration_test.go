package postgres_test

import (
	"context"
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// A suggestion is stored as JSON and must come back as nil when there was none,
// not as an empty struct: the cooldown and the banner both key off nil.
func TestChatRepositorySuggestionRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewChatRepository(pool)

	session, err := repo.CreateSession(ctx, port.ChatSession{Title: "t"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	suggestion := &domain.Suggestion{
		Traps:  []domain.TrapFound{{ID: "spotlight", Name: "Spotlight effect", Quote: "everyone will think", Source: domain.SuggestionSignal}},
		By:     "patton",
		Source: domain.SuggestionSignal,
	}
	if _, err := repo.CreateMessage(ctx, port.ChatMessage{SessionID: session.ID, Role: "assistant", Content: "a", Suggestion: suggestion}); err != nil {
		t.Fatalf("create with suggestion: %v", err)
	}
	if _, err := repo.CreateMessage(ctx, port.ChatMessage{SessionID: session.ID, Role: "assistant", Content: "b"}); err != nil {
		t.Fatalf("create without suggestion: %v", err)
	}

	messages, err := repo.ListMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	var with, without *port.ChatMessage
	for i := range messages {
		if messages[i].Content == "a" {
			with = &messages[i]
		} else {
			without = &messages[i]
		}
	}
	if with.Suggestion == nil || with.Suggestion.By != "patton" || with.Suggestion.Traps[0].Quote != "everyone will think" {
		t.Fatalf("suggestion did not round-trip: %+v", with.Suggestion)
	}
	if without.Suggestion != nil {
		t.Fatalf("no suggestion must read back as nil, got %+v", without.Suggestion)
	}
}
