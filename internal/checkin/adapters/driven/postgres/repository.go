// Package postgres persists check-ins.
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

	"github.com/an4eetos/decision-room/internal/checkin/domain"
	"github.com/an4eetos/decision-room/internal/checkin/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = `id, slot, kind, local_date, mode_id, title, body, session_id, seen_at, created_at`

func (r *Repository) Create(ctx context.Context, c domain.CheckIn) (domain.CheckIn, error) {
	created, err := scan(r.pool.QueryRow(ctx, `
		INSERT INTO checkins (slot, kind, local_date, mode_id, title, body)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+columns,
		c.Slot, string(c.Kind), c.LocalDate, c.ModeID, c.Title, c.Body))

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.CheckIn{}, port.ErrAlreadyFired
	}
	if err != nil {
		return domain.CheckIn{}, fmt.Errorf("create check-in: %w", err)
	}
	return created, nil
}

func (r *Repository) Exists(ctx context.Context, slot string, localDate time.Time) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM checkins WHERE slot = $1 AND local_date = $2)`,
		slot, localDate).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check-in exists: %w", err)
	}
	return exists, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.CheckIn, error) {
	c, err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM checkins WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CheckIn{}, port.ErrNotFound
	}
	if err != nil {
		return domain.CheckIn{}, fmt.Errorf("get check-in: %w", err)
	}
	return c, nil
}

func (r *Repository) Unseen(ctx context.Context, limit int) ([]domain.CheckIn, error) {
	if limit <= 0 {
		limit = 5
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+columns+` FROM checkins
		WHERE seen_at IS NULL
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("unseen check-ins: %w", err)
	}
	defer rows.Close()

	var out []domain.CheckIn
	for rows.Next() {
		c, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) MarkSeen(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE checkins SET seen_at = COALESCE(seen_at, now()) WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("mark check-in seen: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (r *Repository) AttachSession(ctx context.Context, id, sessionID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE checkins SET session_id = $2, seen_at = COALESCE(seen_at, now()) WHERE id = $1`,
		id, sessionID)
	if err != nil {
		return fmt.Errorf("attach session: %w", err)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scan(row scannable) (domain.CheckIn, error) {
	var (
		c    domain.CheckIn
		kind string
	)
	err := row.Scan(&c.ID, &c.Slot, &kind, &c.LocalDate, &c.ModeID, &c.Title, &c.Body, &c.SessionID, &c.SeenAt, &c.CreatedAt)
	if err != nil {
		return domain.CheckIn{}, err
	}
	c.Kind = domain.Kind(kind)
	return c, nil
}
