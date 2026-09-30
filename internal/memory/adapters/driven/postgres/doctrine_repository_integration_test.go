package postgres_test

import (
	"context"
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/adapters/driven/postgres"
)

// The vector column has no fixed dimension and is read back through a text
// cast, so the round trip is the thing most likely to break.
func TestDoctrineRepositoryRoundTripAndPrune(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewDoctrineRepository(pool)

	if err := repo.Save(ctx, "a", "model-1", []float32{0.25, -0.5, 1}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if err := repo.Save(ctx, "b", "model-1", []float32{1, 0}); err != nil {
		t.Fatalf("save b: %v", err)
	}
	// Same hash under another model must stay separate: vectors from two
	// models are dimensionally valid and semantically meaningless together.
	if err := repo.Save(ctx, "a", "model-2", []float32{9}); err != nil {
		t.Fatalf("save a/model-2: %v", err)
	}
	// Re-saving is an upsert, not a conflict error.
	if err := repo.Save(ctx, "a", "model-1", []float32{0.25, -0.5, 1}); err != nil {
		t.Fatalf("re-save a: %v", err)
	}

	got, err := repo.Load(ctx, "model-1")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 vectors for model-1, got %d", len(got))
	}
	if v := got["a"]; len(v) != 3 || v[0] != 0.25 || v[1] != -0.5 || v[2] != 1 {
		t.Fatalf("vector a did not round-trip: %v", v)
	}

	if err := repo.Prune(ctx, "model-1", []string{"a"}); err != nil {
		t.Fatalf("prune: %v", err)
	}
	got, err = repo.Load(ctx, "model-1")
	if err != nil {
		t.Fatalf("load after prune: %v", err)
	}
	if _, ok := got["b"]; ok || len(got) != 1 {
		t.Fatalf("prune should keep only a, got %v", got)
	}

	other, err := repo.Load(ctx, "model-2")
	if err != nil {
		t.Fatalf("load model-2: %v", err)
	}
	if len(other) != 1 {
		t.Fatal("pruning one model must not touch another's vectors")
	}
}
