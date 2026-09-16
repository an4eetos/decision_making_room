package port

import (
	"context"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

type SearchFilter struct {
	Kind *domain.MemoryKind
	Tags []string
}

type MemoryRepository interface {
	Save(ctx context.Context, entry domain.MemoryEntry) error
	SearchSimilar(ctx context.Context, embedding []float32, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	SearchFullText(ctx context.Context, query string, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	ListRecent(ctx context.Context, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	DeleteBySourcePath(ctx context.Context, sourcePath string) error
	SourceContentHash(ctx context.Context, sourcePath string) (string, bool, error)
}
