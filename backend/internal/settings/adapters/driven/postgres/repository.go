// Package postgres persists settings.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/settings/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// maxRows is far above the catalogue; the table holds one row per setting.
const maxRows = 500

func (r *Repository) All(ctx context.Context) ([]port.Stored, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT key, value, secret, updated_at FROM settings ORDER BY key LIMIT $1`, maxRows)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	var out []port.Stored
	for rows.Next() {
		var s port.Stored
		if err := rows.Scan(&s.Key, &s.Value, &s.Secret, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	return out, nil
}

func (r *Repository) Save(ctx context.Context, upserts []port.Stored, deletes []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if len(upserts) > 0 {
		keys := make([]string, len(upserts))
		values := make([]string, len(upserts))
		secrets := make([]bool, len(upserts))
		for i, u := range upserts {
			keys[i], values[i], secrets[i] = u.Key, u.Value, u.Secret
		}
		// One statement for the whole batch.
		if _, err := tx.Exec(ctx, `
			INSERT INTO settings (key, value, secret)
			SELECT * FROM unnest($1::varchar[], $2::varchar[], $3::boolean[])
			ON CONFLICT (key) DO UPDATE
			SET value = EXCLUDED.value, secret = EXCLUDED.secret, updated_at = now()`,
			keys, values, secrets); err != nil {
			return fmt.Errorf("save settings: %w", err)
		}
	}
	if len(deletes) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM settings WHERE key = ANY($1::varchar[])`, deletes); err != nil {
			return fmt.Errorf("delete settings: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
