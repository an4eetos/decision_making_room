package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// DoctrineRepository caches doctrine passage vectors. See port.DoctrineVectorStore.
type DoctrineRepository struct {
	pool *pgxpool.Pool
}

func NewDoctrineRepository(pool *pgxpool.Pool) *DoctrineRepository {
	return &DoctrineRepository{pool: pool}
}

func (r *DoctrineRepository) Load(ctx context.Context, model string) (map[string][]float32, error) {
	// Cast to text: the pool registers no pgvector codec, and the text form is
	// what pgvector.Vector knows how to scan.
	rows, err := r.pool.Query(ctx,
		`SELECT content_hash, embedding::text FROM doctrine_embeddings WHERE embedding_model = $1`, model)
	if err != nil {
		return nil, fmt.Errorf("load doctrine vectors: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]float32)
	for rows.Next() {
		var (
			hash, raw string
			vec       pgvector.Vector
		)
		if err := rows.Scan(&hash, &raw); err != nil {
			return nil, err
		}
		if err := vec.Scan(raw); err != nil {
			return nil, fmt.Errorf("parse doctrine vector %s: %w", hash, err)
		}
		out[hash] = vec.Slice()
	}
	return out, rows.Err()
}

func (r *DoctrineRepository) Save(ctx context.Context, hash, model string, embedding []float32) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO doctrine_embeddings (content_hash, embedding_model, embedding)
		VALUES ($1, $2, $3)
		ON CONFLICT (content_hash, embedding_model) DO UPDATE SET embedding = EXCLUDED.embedding
	`, hash, model, pgvector.NewVector(embedding))
	if err != nil {
		return fmt.Errorf("save doctrine vector: %w", err)
	}
	return nil
}

func (r *DoctrineRepository) Prune(ctx context.Context, model string, keep []string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM doctrine_embeddings
		WHERE embedding_model = $1 AND NOT (content_hash = ANY($2))
	`, model, keep)
	if err != nil {
		return fmt.Errorf("prune doctrine vectors: %w", err)
	}
	return nil
}
