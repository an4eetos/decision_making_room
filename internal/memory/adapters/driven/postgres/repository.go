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

func (r *Repository) SearchFullText(ctx context.Context, query port.TextQuery, limit int, filter port.SearchFilter) ([]domain.MemoryEntry, error) {
	if query.IsZero() {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}

	entries, err := r.searchTSQuery(ctx, query, limit, filter)
	if err != nil {
		return nil, err
	}

	// Trigram fallback for typos and near-misses. Gated on a thin result set so
	// it never competes when real full-text search is working.
	if len(entries) < minFullTextResults {
		fuzzy, err := r.searchTrigram(ctx, query, limit, filter)
		if err != nil {
			return nil, err
		}
		entries = appendUnseen(entries, fuzzy)
	}

	return entries, nil
}

const minFullTextResults = 3

func (r *Repository) searchTSQuery(ctx context.Context, query port.TextQuery, limit int, filter port.SearchFilter) ([]domain.MemoryEntry, error) {
	args := []any{query.English, query.Simple, limit}
	where := []string{`(search_vector @@ to_tsquery('english', $1) OR search_vector_simple @@ to_tsquery('simple', $2))`}
	args, where = appendFilters(args, where, filter, 4)

	// The simple-config vector is weighted at half the english one: it matches
	// proper nouns and codenames the stemmer would mangle, but it also matches
	// stopword noise, so it breaks ties rather than driving the ranking.
	sqlQuery := fmt.Sprintf(`
		SELECT id, kind, title, body, tags, metadata, created_at, updated_at,
		       ts_rank(search_vector, to_tsquery('english', $1))
		         + 0.5 * ts_rank(search_vector_simple, to_tsquery('simple', $2)) AS score
		FROM memories
		WHERE %s
		ORDER BY score DESC
		LIMIT $3
	`, strings.Join(where, " AND "))

	rows, err := r.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search full text: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

func (r *Repository) searchTrigram(ctx context.Context, query port.TextQuery, limit int, filter port.SearchFilter) ([]domain.MemoryEntry, error) {
	if len(query.Terms) == 0 {
		return nil, nil
	}

	needle := strings.Join(query.Terms, " ")
	args := []any{needle, limit}
	// Single %: this string is a Sprintf argument, not a format string.
	where := []string{"title % $1"}
	args, where = appendFilters(args, where, filter, 3)

	sqlQuery := fmt.Sprintf(`
		SELECT id, kind, title, body, tags, metadata, created_at, updated_at,
		       similarity(title, $1) AS score
		FROM memories
		WHERE %s
		ORDER BY score DESC
		LIMIT $2
	`, strings.Join(where, " AND "))

	rows, err := r.pool.Query(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("search trigram: %w", err)
	}
	defer rows.Close()

	return scanEntries(rows)
}

// appendFilters adds the optional kind and tag predicates, continuing the
// placeholder numbering from nextIdx.
func appendFilters(args []any, where []string, filter port.SearchFilter, nextIdx int) ([]any, []string) {
	if filter.Kind != nil {
		where = append(where, fmt.Sprintf("kind = $%d", nextIdx))
		args = append(args, string(*filter.Kind))
		nextIdx++
	}
	if len(filter.Tags) > 0 {
		where = append(where, fmt.Sprintf("tags && $%d", nextIdx))
		args = append(args, filter.Tags)
	}
	return args, where
}

func appendUnseen(entries, extra []domain.MemoryEntry) []domain.MemoryEntry {
	seen := make(map[uuid.UUID]struct{}, len(entries))
	for _, e := range entries {
		seen[e.ID] = struct{}{}
	}
	for _, e := range extra {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		entries = append(entries, e)
	}
	return entries
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
