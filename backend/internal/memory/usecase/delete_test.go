package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

type stubDeleteRepo struct {
	stubRepo
	deleted uuid.UUID
	err     error
}

func (s *stubDeleteRepo) DeleteByID(_ context.Context, id uuid.UUID) error {
	s.deleted = id
	return s.err
}

func TestDeleteMemory(t *testing.T) {
	t.Parallel()

	repo := &stubDeleteRepo{}
	del := NewDelete(repo)
	id := uuid.New().String()

	if err := del.Execute(context.Background(), id); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if repo.deleted.String() != id {
		t.Fatalf("deleted = %s, want %s", repo.deleted, id)
	}
}

func TestDeleteMemoryInvalidID(t *testing.T) {
	t.Parallel()

	del := NewDelete(&stubDeleteRepo{})
	err := del.Execute(context.Background(), "not-a-uuid")
	if err == nil || !strings.Contains(err.Error(), "invalid memory id") {
		t.Fatalf("expected invalid memory id error, got %v", err)
	}
}

func TestDeleteMemoryNotFound(t *testing.T) {
	t.Parallel()

	repo := &stubDeleteRepo{err: port.ErrMemoryNotFound}
	del := NewDelete(repo)

	err := del.Execute(context.Background(), uuid.New().String())
	if !errors.Is(err, port.ErrMemoryNotFound) {
		t.Fatalf("expected not found error, got %v", err)
	}
}
