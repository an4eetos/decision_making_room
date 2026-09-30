package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

type UpdateInput struct {
	ID    string
	Kind  domain.MemoryKind
	Title string
	Body  string
	Tags  []string
}

type Update struct {
	repo     port.MemoryRepository
	embedder port.Embedder
}

func NewUpdate(repo port.MemoryRepository, embedder port.Embedder) *Update {
	return &Update{repo: repo, embedder: embedder}
}

// Execute re-embeds the edited content, since an edit that leaves the old
// vector in place would keep matching a query on text that no longer exists.
func (u *Update) Execute(ctx context.Context, input UpdateInput) error {
	id, err := uuid.Parse(input.ID)
	if err != nil {
		return fmt.Errorf("invalid memory id")
	}
	if strings.TrimSpace(input.Body) == "" {
		return fmt.Errorf("body is required")
	}
	if input.Kind == "" {
		return fmt.Errorf("kind is required")
	}

	embedText := input.Body
	if input.Title != "" {
		embedText = input.Title + "\n\n" + input.Body
	}
	embedding, err := u.embedder.Embed(ctx, embedText)
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}

	entry := domain.MemoryEntry{
		ID:             id,
		Kind:           input.Kind,
		Title:          input.Title,
		Body:           input.Body,
		Tags:           input.Tags,
		Embedding:      embedding,
		EmbeddingModel: u.embedder.ModelID(),
		EmbeddingDim:   len(embedding),
	}

	return u.repo.UpdateByID(ctx, entry)
}
