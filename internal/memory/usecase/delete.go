package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

type Delete struct {
	repo port.MemoryRepository
}

func NewDelete(repo port.MemoryRepository) *Delete {
	return &Delete{repo: repo}
}

func (u *Delete) Execute(ctx context.Context, memoryID string) error {
	id, err := uuid.Parse(memoryID)
	if err != nil {
		return fmt.Errorf("invalid memory id")
	}
	return u.repo.DeleteByID(ctx, id)
}
