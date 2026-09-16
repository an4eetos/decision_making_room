package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Save(ctx context.Context, entry domain.MemoryEntry) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	now := time.Now().UTC()
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
	if entry.Tags == nil {
		entry.Tags = []string{}
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]string{}
	}

	metadata, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}

	var embedding any
	if len(entry.Embedding) > 0 {
		embedding = pgvector.NewVector(entry.Embedding)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO memories (id, kind, title, body, tags, metadata, embedding, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, entry.ID, string(entry.Kind), entry.Title, entry.Body, entry.Tags, metadata, embedding, entry.CreatedAt, entry.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert memory: %w", err)
	}

	return nil
}


func (r *Repository) SearchSimilar(ctx context.Context, embedding []float32, limit int, filter port.SearchFilter) ([]domain.MemoryEntry, error) {
	if limit <= 0 {
		limit = 8
	}

	args := []any{pgvector.NewVector(embedding), limit}
	where := []string{"embedding IS NOT NULL"}
	argIdx := 3

	if filter.Kind != nil {
		where = append(where, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, string(*filter.Kind))
		argIdx++
	}

	if len(filter.Tags) > 0 {
		where = append(where, fmt.Sprintf("tags && $%d", argIdx))
		args = append(args, filter.Tags)
	}

	query := fmt.Sprintf(`
		SELECT id, kind, title, body, tags, metadata, created_at, updated_at,
		       1 - (embedding <=> $1) AS score
		FROM memories
		WHERE %s
		ORDER BY embedding <=> $1
		LIMIT $2
	`, strings.Join(where, " AND "))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search similar: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (r *Repository) ListRecent(ctx context.Context, limit int, filter port.SearchFilter) ([]domain.MemoryEntry, error) {
	if limit <= 0 {
		limit = 20
	}

	args := []any{limit}
	where := []string{"TRUE"}
	argIdx := 2

	if filter.Kind != nil {
		where = append(where, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, string(*filter.Kind))
		argIdx++
	}

	if len(filter.Tags) > 0 {
		where = append(where, fmt.Sprintf("tags && $%d", argIdx))
		args = append(args, filter.Tags)
	}

	query := fmt.Sprintf(`
		SELECT id, kind, title, body, tags, metadata, created_at, updated_at, 0::float8 AS score
		FROM memories
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $1
	`, strings.Join(where, " AND "))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list recent: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (r *Repository) DeleteBySourcePath(ctx context.Context, sourcePath string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM memories
		WHERE metadata->>'source_path' = $1
	`, sourcePath)
	if err != nil {
		return fmt.Errorf("delete by source path: %w", err)
	}
	return nil
}

func (r *Repository) SourceContentHash(ctx context.Context, sourcePath string) (string, bool, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `
		SELECT metadata->>'content_hash'
		FROM memories
		WHERE metadata->>'source_path' = $1
		LIMIT 1
	`, sourcePath).Scan(&hash)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("source content hash: %w", err)
	}
	if hash == "" {
		return "", false, nil
	}
	return hash, true, nil
}

func (r *Repository) SearchFullText(ctx context.Context, query string, limit int, filter port.SearchFilter) ([]domain.MemoryEntry, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}

	args := []any{query, limit}
	where := []string{"search_vector @@ websearch_to_tsquery('english', $1)"}
	argIdx := 3

	if filter.Kind != nil {
		where = append(where, fmt.Sprintf("kind = $%d", argIdx))
		args = append(args, string(*filter.Kind))
		argIdx++
	}

	if len(filter.Tags) > 0 {
		where = append(where, fmt.Sprintf("tags && $%d", argIdx))
		args = append(args, filter.Tags)
	}

	sqlQuery := fmt.Sprintf(`
		SELECT id, kind, title, body, tags, metadata, created_at, updated_at,
		       ts_rank(search_vector, websearch_to_tsquery('english', $1)) AS score
		FROM memories
		WHERE %s
		ORDER BY score DESC
		LIMIT $2
	`, strings.Join(where, " AND "))

	rows, err := r.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search full text: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func scanEntries(rows pgx.Rows) ([]domain.MemoryEntry, error) {
	var entries []domain.MemoryEntry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanEntry(row scannable) (domain.MemoryEntry, error) {
	var entry domain.MemoryEntry
	var kind string
	var metadataJSON []byte

	err := row.Scan(
		&entry.ID,
		&kind,
		&entry.Title,
		&entry.Body,
		&entry.Tags,
		&metadataJSON,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&entry.Score,
	)
	if err != nil {
		return domain.MemoryEntry{}, err
	}

	entry.Kind = domain.MemoryKind(kind)
	if len(metadataJSON) > 0 {
		_ = json.Unmarshal(metadataJSON, &entry.Metadata)
	}
	if entry.Metadata == nil {
		entry.Metadata = map[string]string{}
	}

	return entry, nil
}
