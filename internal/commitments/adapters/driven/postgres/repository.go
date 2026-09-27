// Package postgres persists commitments.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = `id, text, status, due_at, source, session_id, message_id, mode_id,
	confidence, fingerprint, created_at, updated_at, resolved_at`

func (r *Repository) Create(ctx context.Context, c domain.Commitment) (domain.Commitment, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO commitments
			(text, status, due_at, source, session_id, message_id, mode_id, confidence, fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+columns,
		c.Text, string(c.Status), c.DueAt, string(c.Source), c.SessionID, c.MessageID,
		c.ModeID, c.Confidence, c.Fingerprint)

	created, err := scan(row)
	if isUniqueViolation(err) {
		// The partial unique index on live fingerprints is the dedupe. Surfacing
		// it as a typed error lets the caller treat "already tracked" as success.
		return domain.Commitment{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Commitment{}, fmt.Errorf("create commitment: %w", err)
	}
	return created, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.Commitment, error) {
	c, err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM commitments WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Commitment{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Commitment{}, fmt.Errorf("get commitment: %w", err)
	}
	return c, nil
}

func (r *Repository) List(ctx context.Context, statuses []domain.Status, limit int) ([]domain.Commitment, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `SELECT ` + columns + ` FROM commitments`
	args := []any{limit}
	if len(statuses) > 0 {
		values := make([]string, len(statuses))
		for i, s := range statuses {
			values[i] = string(s)
		}
		query += ` WHERE status = ANY($2)`
		args = append(args, values)
	}
	// Proposals first so they are seen, then by due date, then most recent.
	query += ` ORDER BY (status = 'proposed') DESC, due_at ASC NULLS LAST, updated_at DESC LIMIT $1`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list commitments: %w", err)
	}
	defer rows.Close()

	var out []domain.Commitment
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetStatus stamps resolved_at when a commitment leaves the list and clears it
// when one comes back, so "reopen" does not leave a stale resolution time.
func (r *Repository) SetStatus(ctx context.Context, id uuid.UUID, status domain.Status) (domain.Commitment, error) {
	c, err := scan(r.pool.QueryRow(ctx, `
		UPDATE commitments SET
			status = $2,
			updated_at = now(),
			resolved_at = CASE WHEN $2 IN ('done', 'dropped') THEN now() ELSE NULL END
		WHERE id = $1
		RETURNING `+columns, id, string(status)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Commitment{}, port.ErrNotFound
	}
	if isUniqueViolation(err) {
		// Reopening something while an identical one is already live.
		return domain.Commitment{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Commitment{}, fmt.Errorf("set commitment status: %w", err)
	}
	return c, nil
}

func (r *Repository) UpdateText(ctx context.Context, id uuid.UUID, text, fingerprint string, due *time.Time) (domain.Commitment, error) {
	c, err := scan(r.pool.QueryRow(ctx, `
		UPDATE commitments SET text = $2, fingerprint = $3, due_at = $4, updated_at = now()
		WHERE id = $1
		RETURNING `+columns, id, text, fingerprint, due))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Commitment{}, port.ErrNotFound
	}
	if isUniqueViolation(err) {
		return domain.Commitment{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Commitment{}, fmt.Errorf("update commitment: %w", err)
	}
	return c, nil
}

// MarkStale is plain SQL with no model call: whether something has gone
// untouched for two weeks is a fact, not a judgement.
func (r *Repository) MarkStale(ctx context.Context, cutoff time.Time) ([]domain.Commitment, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE commitments SET status = 'stale'
		WHERE status = 'open' AND updated_at < $1
		RETURNING `+columns, cutoff)
	if err != nil {
		return nil, fmt.Errorf("mark stale: %w", err)
	}
	defer rows.Close()

	var out []domain.Commitment
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scan(row scannable) (domain.Commitment, error) {
	var (
		c              domain.Commitment
		status, source string
	)
	err := row.Scan(&c.ID, &c.Text, &status, &c.DueAt, &source, &c.SessionID, &c.MessageID,
		&c.ModeID, &c.Confidence, &c.Fingerprint, &c.CreatedAt, &c.UpdatedAt, &c.ResolvedAt)
	if err != nil {
		return domain.Commitment{}, err
	}
	c.Status = domain.Status(status)
	c.Source = domain.Source(source)
	return c, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
