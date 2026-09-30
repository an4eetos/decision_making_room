package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

type stubUpdateRepo struct {
	stubRepo
	updated domain.MemoryEntry
	err     error
}

func (s *stubUpdateRepo) UpdateByID(_ context.Context, entry domain.MemoryEntry) error {
	s.updated = entry
	return s.err
}

func TestUpdateMemory(t *testing.T) {
	t.Parallel()

	repo := &stubUpdateRepo{}
	upd := NewUpdate(repo, stubEmbedder{})
	id := uuid.New().String()

	err := upd.Execute(context.Background(), UpdateInput{
		ID:    id,
		Kind:  domain.KindNote,
		Title: "revised title",
		Body:  "revised body",
		Tags:  []string{"a", "b"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.updated.ID.String() != id {
		t.Fatalf("updated.ID = %s, want %s", repo.updated.ID, id)
	}
	if repo.updated.Title != "revised title" || repo.updated.Body != "revised body" {
		t.Fatalf("updated entry = %+v", repo.updated)
	}
	if len(repo.updated.Embedding) == 0 {
		t.Fatalf("expected re-embedded content, got empty embedding")
	}
}

func TestUpdateMemoryInvalidID(t *testing.T) {
	t.Parallel()

	upd := NewUpdate(&stubUpdateRepo{}, stubEmbedder{})
	err := upd.Execute(context.Background(), UpdateInput{
		ID:   "not-a-uuid",
		Kind: domain.KindNote,
		Body: "body",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid memory id") {
		t.Fatalf("expected invalid memory id error, got %v", err)
	}
}

func TestUpdateMemoryRequiresBody(t *testing.T) {
	t.Parallel()

	upd := NewUpdate(&stubUpdateRepo{}, stubEmbedder{})
	err := upd.Execute(context.Background(), UpdateInput{
		ID:   uuid.New().String(),
		Kind: domain.KindNote,
		Body: "   ",
	})
	if err == nil || !strings.Contains(err.Error(), "body is required") {
		t.Fatalf("expected body is required error, got %v", err)
	}
}

func TestUpdateMemoryNotFound(t *testing.T) {
	t.Parallel()

	repo := &stubUpdateRepo{err: port.ErrMemoryNotFound}
	upd := NewUpdate(repo, stubEmbedder{})

	err := upd.Execute(context.Background(), UpdateInput{
		ID:   uuid.New().String(),
		Kind: domain.KindNote,
		Body: "body",
	})
	if !errors.Is(err, port.ErrMemoryNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}
