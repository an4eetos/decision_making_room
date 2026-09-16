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
	SearchFullText(ctx context.Context, query TextQuery, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	ListRecent(ctx context.Context, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	DeleteBySourcePath(ctx context.Context, sourcePath string) error
	SourceContentHash(ctx context.Context, sourcePath string) (string, bool, error)
}

// TextQuery is a question already reduced to tsquery expressions. The repository
// takes it prepared rather than raw, because turning prose into something
// Postgres can match is a decision about retrieval, not about storage.
type TextQuery struct {
	// English and Simple are OR-joined tsquery expressions for the 'english' and
	// 'simple' configurations respectively.
	English string
	Simple  string
	// Terms is the same content words unjoined, for the trigram fallback.
	Terms []string
}

func (q TextQuery) IsZero() bool { return q.English == "" && q.Simple == "" }
