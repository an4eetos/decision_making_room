// Package postgres persists the owner password and sessions.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/auth/domain"
	"github.com/an4eetos/decision-room/internal/auth/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// maxSessions caps the sessions list. One person has a handful of devices; the
// cap only matters if something is minting sessions in a loop.
const maxSessions = 200

const sessionColumns = `id, token_hash, client, created_at, last_seen_at, expires_at`

func (r *Repository) OwnerHash(ctx context.Context) (string, error) {
	var hash string
	err := r.pool.QueryRow(ctx, `SELECT password_hash FROM auth_owner WHERE id = 1`).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", port.ErrNoOwner
	}
	if err != nil {
		return "", fmt.Errorf("read owner: %w", err)
	}
	return hash, nil
}

func (r *Repository) CreateOwner(ctx context.Context, hash string) error {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO auth_owner (id, password_hash) VALUES (1, $1) ON CONFLICT (id) DO NOTHING`, hash)
	if err != nil {
		return fmt.Errorf("create owner: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrOwnerExists
	}
	return nil
}

func (r *Repository) ReplaceOwner(ctx context.Context, hash string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("replace owner: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO auth_owner (id, password_hash) VALUES (1, $1)
		ON CONFLICT (id) DO UPDATE SET password_hash = EXCLUDED.password_hash, updated_at = now()`,
		hash); err != nil {
		return fmt.Errorf("replace owner: %w", err)
	}
	// A new password is the recovery path for a lost device, so every
	// existing session goes with the old one. Deliberately the whole table:
	// it is one person's devices, a few rows.
	if _, err := tx.Exec(ctx, `DELETE FROM auth_sessions`); err != nil {
		return fmt.Errorf("revoke sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("replace owner: %w", err)
	}
	return nil
}

func (r *Repository) CreateSession(ctx context.Context, s domain.Session) (domain.Session, error) {
	created, err := scanSession(r.pool.QueryRow(ctx, `
		INSERT INTO auth_sessions (token_hash, client, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $3, $4)
		RETURNING `+sessionColumns,
		s.TokenHash, s.Client, s.CreatedAt, s.ExpiresAt))
	if err != nil {
		return domain.Session{}, fmt.Errorf("create session: %w", err)
	}
	return created, nil
}

func (r *Repository) SessionByTokenHash(ctx context.Context, hash []byte, now time.Time) (domain.Session, error) {
	s, err := scanSession(r.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM auth_sessions WHERE token_hash = $1 AND expires_at > $2`,
		hash, now))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, port.ErrSessionNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("read session: %w", err)
	}
	return s, nil
}

func (r *Repository) TouchSession(ctx context.Context, id uuid.UUID, seenAt, expiresAt time.Time) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE auth_sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1`,
		id, seenAt, expiresAt); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

func (r *Repository) ListSessions(ctx context.Context, now time.Time) ([]domain.Session, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+sessionColumns+` FROM auth_sessions
		WHERE expires_at > $1
		ORDER BY last_seen_at DESC
		LIMIT $2`, now, maxSessions)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []domain.Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return out, nil
}

func (r *Repository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM auth_sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrSessionNotFound
	}
	return nil
}

func (r *Repository) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM auth_sessions WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanSession(row pgx.Row) (domain.Session, error) {
	var s domain.Session
	err := row.Scan(&s.ID, &s.TokenHash, &s.Client, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt)
	return s, err
}
