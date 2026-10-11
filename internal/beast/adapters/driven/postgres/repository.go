// Package postgres persists beast mode's board.
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

	"github.com/an4eetos/decision-room/internal/beast/domain"
	"github.com/an4eetos/decision-room/internal/beast/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

const columns = `id, session_id, message_id, seq, text, minutes, deadline_at, status,
	expiries, commitment_id, created_at, resolved_at`

// createAttempts covers two inserts racing for the same number: the unique
// (session_id, seq) index rejects the loser, which simply takes the next one.
const createAttempts = 3

func (r *Repository) Create(ctx context.Context, t domain.Task) (domain.Task, error) {
	var (
		created domain.Task
		err     error
	)
	for range createAttempts {
		created, err = scan(r.pool.QueryRow(ctx, `
			INSERT INTO beast_tasks
				(session_id, message_id, seq, text, minutes, deadline_at, status, commitment_id)
			SELECT $1, $2, COALESCE(MAX(seq), 0) + 1, $3, $4, $5, $6, $7
			FROM beast_tasks WHERE session_id = $1
			RETURNING `+columns,
			t.SessionID, t.MessageID, t.Text, t.Minutes, t.DeadlineAt, string(t.Status), t.CommitmentID))
		if !isUniqueViolation(err) {
			break
		}
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("create beast task: %w", err)
	}
	return created, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	t, err := scan(r.pool.QueryRow(ctx, `SELECT `+columns+` FROM beast_tasks WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("get beast task: %w", err)
	}
	return t, nil
}

func (r *Repository) ListBySession(ctx context.Context, sessionID uuid.UUID) ([]domain.Task, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+columns+` FROM beast_tasks WHERE session_id = $1 ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list beast tasks: %w", err)
	}
	defer rows.Close()

	var out []domain.Task
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) Update(ctx context.Context, t domain.Task) (domain.Task, error) {
	updated, err := scan(r.pool.QueryRow(ctx, `
		UPDATE beast_tasks SET
			text = $2,
			minutes = $3,
			deadline_at = $4,
			status = $5,
			resolved_at = CASE WHEN $5 = 'open' THEN NULL ELSE COALESCE(resolved_at, now()) END
		WHERE id = $1
		RETURNING `+columns, t.ID, t.Text, t.Minutes, t.DeadlineAt, string(t.Status)))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("update beast task: %w", err)
	}
	return updated, nil
}

func (r *Repository) Rearm(ctx context.Context, id uuid.UUID, now, deadline time.Time) (domain.Task, bool, error) {
	t, err := scan(r.pool.QueryRow(ctx, `
		UPDATE beast_tasks SET expiries = expiries + 1, deadline_at = $3
		WHERE id = $1 AND status = 'open' AND deadline_at <= $2
		RETURNING `+columns, id, now, deadline))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Task{}, false, nil
	}
	if err != nil {
		return domain.Task{}, false, fmt.Errorf("rearm beast task: %w", err)
	}
	return t, true, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scan(row scannable) (domain.Task, error) {
	var (
		t      domain.Task
		status string
	)
	err := row.Scan(&t.ID, &t.SessionID, &t.MessageID, &t.Seq, &t.Text, &t.Minutes, &t.DeadlineAt,
		&status, &t.Expiries, &t.CommitmentID, &t.CreatedAt, &t.ResolvedAt)
	if err != nil {
		return domain.Task{}, err
	}
	t.Status = domain.Status(status)
	return t, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
