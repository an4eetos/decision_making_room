package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

// SaveSnapshot replaces the day's snapshot: the last write of the day is the
// day's map.
func (r *Repository) SaveSnapshot(ctx context.Context, s domain.Snapshot) error {
	day, err := time.Parse("2006-01-02", s.Day)
	if err != nil {
		return fmt.Errorf("snapshot day %q: %w", s.Day, err)
	}
	state, err := json.Marshal(s.Theatre)
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO campaign_snapshots (day, taken_at, state)
		VALUES ($1, $2, $3)
		ON CONFLICT (day) DO UPDATE SET taken_at = EXCLUDED.taken_at, state = EXCLUDED.state`,
		day, s.TakenAt, state)
	if err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

func (r *Repository) GetSnapshot(ctx context.Context, day string) (domain.Snapshot, error) {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("snapshot day %q: %w", day, err)
	}
	s, err := scanSnapshot(r.pool.QueryRow(ctx,
		`SELECT day, taken_at, state FROM campaign_snapshots WHERE day = $1`, d))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Snapshot{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("get snapshot: %w", err)
	}
	return s, nil
}

// GetSnapshotBefore is the one-row read the sitrep makes on every answer.
func (r *Repository) GetSnapshotBefore(ctx context.Context, day string) (domain.Snapshot, error) {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("snapshot day %q: %w", day, err)
	}
	s, err := scanSnapshot(r.pool.QueryRow(ctx,
		`SELECT day, taken_at, state FROM campaign_snapshots WHERE day < $1 ORDER BY day DESC LIMIT 1`, d))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Snapshot{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("get snapshot before: %w", err)
	}
	return s, nil
}

// ListSnapshots returns the latest limit days, oldest first, which is the
// order a timelapse plays in.
func (r *Repository) ListSnapshots(ctx context.Context, limit int) ([]domain.Snapshot, error) {
	if limit <= 0 {
		limit = 400
	}
	rows, err := r.pool.Query(ctx, `
		SELECT day, taken_at, state FROM (
			SELECT day, taken_at, state FROM campaign_snapshots ORDER BY day DESC LIMIT $1
		) recent ORDER BY day ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	defer rows.Close()

	var out []domain.Snapshot
	for rows.Next() {
		s, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanSnapshot(row scannable) (domain.Snapshot, error) {
	var (
		s     domain.Snapshot
		day   time.Time
		state []byte
	)
	if err := row.Scan(&day, &s.TakenAt, &state); err != nil {
		return domain.Snapshot{}, err
	}
	if err := json.Unmarshal(state, &s.Theatre); err != nil {
		return domain.Snapshot{}, fmt.Errorf("decode snapshot: %w", err)
	}
	s.Day = day.Format("2006-01-02")
	s.Theatre.Day = s.Day
	return s, nil
}
