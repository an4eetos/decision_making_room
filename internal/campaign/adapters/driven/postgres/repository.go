// Package postgres persists the campaign.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const frontColumns = `id, name, status, position, created_at, updated_at`

// CreateFront appends the front after the existing ones.
func (r *Repository) CreateFront(ctx context.Context, name string) (domain.Front, error) {
	f, err := scanFront(r.pool.QueryRow(ctx, `
		INSERT INTO fronts (name, position)
		VALUES ($1, COALESCE((SELECT max(position) + 1 FROM fronts), 0))
		RETURNING `+frontColumns, name))
	if isUniqueViolation(err) {
		return domain.Front{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Front{}, fmt.Errorf("create front: %w", err)
	}
	return f, nil
}

func (r *Repository) UpdateFront(ctx context.Context, f domain.Front) (domain.Front, error) {
	updated, err := scanFront(r.pool.QueryRow(ctx, `
		UPDATE fronts SET name = $2, status = $3, position = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+frontColumns, f.ID, f.Name, string(f.Status), f.Position))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Front{}, port.ErrNotFound
	case isUniqueViolation(err):
		return domain.Front{}, port.ErrDuplicate
	case err != nil:
		return domain.Front{}, fmt.Errorf("update front: %w", err)
	}
	return updated, nil
}

func (r *Repository) GetFront(ctx context.Context, id uuid.UUID) (domain.Front, error) {
	f, err := scanFront(r.pool.QueryRow(ctx, `SELECT `+frontColumns+` FROM fronts WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Front{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Front{}, fmt.Errorf("get front: %w", err)
	}
	return f, nil
}

func (r *Repository) ListFronts(ctx context.Context, activeOnly bool) ([]domain.Front, error) {
	query := `SELECT ` + frontColumns + ` FROM fronts`
	if activeOnly {
		query += ` WHERE status = 'active'`
	}
	query += ` ORDER BY (status = 'active') DESC, position, created_at`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list fronts: %w", err)
	}
	defer rows.Close()

	var out []domain.Front
	for rows.Next() {
		f, err := scanFront(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

const itemColumns = `id, type, front_id, objective_id, text, status, kind, strength,
	strength_confirmed, answer, due_at, source, session_id, message_id, confidence,
	fingerprint, created_at, updated_at, resolved_at`

func (r *Repository) CreateItem(ctx context.Context, it domain.Item) (domain.Item, error) {
	created, err := scanItem(r.pool.QueryRow(ctx, `
		INSERT INTO campaign_items
			(type, front_id, objective_id, text, status, kind, strength, strength_confirmed,
			 answer, due_at, source, session_id, message_id, confidence, fingerprint,
			 resolved_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
			CASE WHEN $5 IN ('resolved', 'withdrawn', 'dropped') THEN now() END)
		RETURNING `+itemColumns,
		string(it.Type), it.FrontID, it.ObjectiveID, it.Text, string(it.Status),
		nullKind(it.Kind), nullStrength(it.Strength), it.StrengthConfirmed, it.Answer,
		it.DueAt, string(it.Source), it.SessionID, it.MessageID, it.Confidence, it.Fingerprint))
	if isUniqueViolation(err) {
		return domain.Item{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("create campaign item: %w", err)
	}
	return created, nil
}

func (r *Repository) GetItem(ctx context.Context, id uuid.UUID) (domain.Item, error) {
	it, err := scanItem(r.pool.QueryRow(ctx, `SELECT `+itemColumns+` FROM campaign_items WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("get campaign item: %w", err)
	}
	return it, nil
}

// UpdateItem stamps resolved_at when an item leaves the map and clears it when
// one comes back, keeping the first stamp if it was already resolved.
func (r *Repository) UpdateItem(ctx context.Context, it domain.Item) (domain.Item, error) {
	updated, err := scanItem(r.pool.QueryRow(ctx, `
		UPDATE campaign_items SET
			front_id = $2, objective_id = $3, text = $4, status = $5, kind = $6,
			strength = $7, strength_confirmed = $8, answer = $9, due_at = $10,
			fingerprint = $11, updated_at = now(),
			resolved_at = CASE
				WHEN $5 IN ('resolved', 'withdrawn', 'dropped') THEN COALESCE(resolved_at, now())
				ELSE NULL END
		WHERE id = $1
		RETURNING `+itemColumns,
		it.ID, it.FrontID, it.ObjectiveID, it.Text, string(it.Status), nullKind(it.Kind),
		nullStrength(it.Strength), it.StrengthConfirmed, it.Answer, it.DueAt, it.Fingerprint))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Item{}, port.ErrNotFound
	case isUniqueViolation(err):
		return domain.Item{}, port.ErrDuplicate
	case err != nil:
		return domain.Item{}, fmt.Errorf("update campaign item: %w", err)
	}
	return updated, nil
}

func (r *Repository) ListItems(ctx context.Context, statuses []domain.Status, limit int) ([]domain.Item, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT ` + itemColumns + ` FROM campaign_items`
	args := []any{limit}
	if len(statuses) > 0 {
		values := make([]string, len(statuses))
		for i, s := range statuses {
			values[i] = string(s)
		}
		query += ` WHERE status = ANY($2)`
		args = append(args, values)
	}
	// Proposals first so they are seen; then objectives before what opposes
	// them; then by due date and recency.
	query += ` ORDER BY (status = 'proposed') DESC,
		CASE type WHEN 'objective' THEN 0 WHEN 'obstacle' THEN 1 ELSE 2 END,
		due_at ASC NULLS LAST, updated_at DESC
		LIMIT $1`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list campaign items: %w", err)
	}
	defer rows.Close()

	var out []domain.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanFront(row scannable) (domain.Front, error) {
	var (
		f      domain.Front
		status string
	)
	if err := row.Scan(&f.ID, &f.Name, &status, &f.Position, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return domain.Front{}, err
	}
	f.Status = domain.FrontStatus(status)
	return f, nil
}

func scanItem(row scannable) (domain.Item, error) {
	var (
		it                  domain.Item
		typ, status, source string
		kind                *string
		strength            *int16
	)
	err := row.Scan(&it.ID, &typ, &it.FrontID, &it.ObjectiveID, &it.Text, &status, &kind,
		&strength, &it.StrengthConfirmed, &it.Answer, &it.DueAt, &source, &it.SessionID,
		&it.MessageID, &it.Confidence, &it.Fingerprint, &it.CreatedAt, &it.UpdatedAt,
		&it.ResolvedAt)
	if err != nil {
		return domain.Item{}, err
	}
	it.Type = domain.Type(typ)
	it.Status = domain.Status(status)
	it.Source = domain.Source(source)
	if kind != nil {
		it.Kind = domain.Kind(*kind)
	}
	if strength != nil {
		it.Strength = int(*strength)
	}
	return it, nil
}

// The kind and strength columns are NULL on everything but obstacles, which a
// check constraint enforces; the zero values map to NULL.
func nullKind(k domain.Kind) *string {
	if k == "" {
		return nil
	}
	s := string(k)
	return &s
}

func nullStrength(s int) *int16 {
	if s == 0 {
		return nil
	}
	v := int16(s)
	return &v
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
