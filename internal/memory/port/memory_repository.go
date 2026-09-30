package port

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

var ErrMemoryNotFound = errors.New("memory not found")

type SearchFilter struct {
	// EmbeddingModel restricts vector search to rows embedded by this model.
	// Empty searches all of them, which is only correct when the table is known
	// to be uniform.
	EmbeddingModel string

	Kind *domain.MemoryKind
	Tags []string
}

type MemoryRepository interface {
	Save(ctx context.Context, entry domain.MemoryEntry) error
	SearchSimilar(ctx context.Context, embedding []float32, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	SearchFullText(ctx context.Context, query TextQuery, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)

	// CountByEmbeddingModel and the reindex pair support switching embedding
	// providers without silently corrupting search.
	CountByEmbeddingModel(ctx context.Context) (map[string]int, error)
	ListStale(ctx context.Context, currentModel string, limit int) ([]domain.MemoryEntry, error)
	UpdateEmbedding(ctx context.Context, id uuid.UUID, embedding []float32, model string) error
	ListRecent(ctx context.Context, limit int, filter SearchFilter) ([]domain.MemoryEntry, error)
	DeleteBySourcePath(ctx context.Context, sourcePath string) error
	// DeleteByID returns ErrMemoryNotFound when no row matches, so callers can
	// tell a no-op delete from a real one.
	DeleteByID(ctx context.Context, id uuid.UUID) error
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
