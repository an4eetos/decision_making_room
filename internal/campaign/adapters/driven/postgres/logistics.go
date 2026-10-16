package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

const sortieColumns = `id, front_id, order_id, minutes, day, note, created_at`

func (r *Repository) CreateSortie(ctx context.Context, s domain.Sortie) (domain.Sortie, error) {
	day, err := parseDay(s.Day)
	if err != nil {
		return domain.Sortie{}, err
	}
	created, err := scanSortie(r.pool.QueryRow(ctx, `
		INSERT INTO sorties (front_id, order_id, minutes, day, note)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+sortieColumns, s.FrontID, s.OrderID, s.Minutes, day, s.Note))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		// The front or the order is not there.
		return domain.Sortie{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Sortie{}, fmt.Errorf("create sortie: %w", err)
	}
	return created, nil
}

func (r *Repository) DeleteSortie(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sorties WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete sortie: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (r *Repository) ListSorties(ctx context.Context, since string) ([]domain.Sortie, error) {
	day, err := parseDay(since)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+sortieColumns+` FROM sorties
		WHERE day >= $1
		ORDER BY day DESC, created_at DESC`, day)
	if err != nil {
		return nil, fmt.Errorf("list sorties: %w", err)
	}
	defer rows.Close()

	out := []domain.Sortie{}
	for rows.Next() {
		s, err := scanSortie(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) FirstSortieDay(ctx context.Context) (string, error) {
	var first *time.Time
	if err := r.pool.QueryRow(ctx, `SELECT min(day) FROM sorties`).Scan(&first); err != nil {
		return "", fmt.Errorf("first sortie: %w", err)
	}
	if first == nil {
		return "", nil
	}
	return first.Format("2006-01-02"), nil
}

func (r *Repository) SaveEnergy(ctx context.Context, e domain.EnergyReport) (domain.EnergyReport, error) {
	day, err := parseDay(e.Day)
	if err != nil {
		return domain.EnergyReport{}, err
	}
	saved, err := scanEnergy(r.pool.QueryRow(ctx, `
		INSERT INTO energy_reports (day, level)
		VALUES ($1, $2)
		ON CONFLICT (day) DO UPDATE SET level = EXCLUDED.level, reported_at = now()
		RETURNING day, level, reported_at`, day, e.Level))
	if err != nil {
		return domain.EnergyReport{}, fmt.Errorf("save energy: %w", err)
	}
	return saved, nil
}

func (r *Repository) DeleteEnergy(ctx context.Context, day string) error {
	d, err := parseDay(day)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM energy_reports WHERE day = $1`, d)
	if err != nil {
		return fmt.Errorf("delete energy: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (r *Repository) ListEnergy(ctx context.Context, since string) ([]domain.EnergyReport, error) {
	day, err := parseDay(since)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `
		SELECT day, level, reported_at FROM energy_reports
		WHERE day >= $1
		ORDER BY day`, day)
	if err != nil {
		return nil, fmt.Errorf("list energy: %w", err)
	}
	defer rows.Close()

	out := []domain.EnergyReport{}
	for rows.Next() {
		e, err := scanEnergy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func parseDay(day string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return time.Time{}, fmt.Errorf("day %q: want YYYY-MM-DD", day)
	}
	return d, nil
}

func scanSortie(row scannable) (domain.Sortie, error) {
	var (
		s   domain.Sortie
		day time.Time
	)
	if err := row.Scan(&s.ID, &s.FrontID, &s.OrderID, &s.Minutes, &day, &s.Note, &s.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Sortie{}, port.ErrNotFound
		}
		return domain.Sortie{}, err
	}
	s.Day = day.Format("2006-01-02")
	return s, nil
}

func scanEnergy(row scannable) (domain.EnergyReport, error) {
	var (
		e     domain.EnergyReport
		day   time.Time
		level int16
	)
	if err := row.Scan(&day, &level, &e.ReportedAt); err != nil {
		return domain.EnergyReport{}, err
	}
	e.Day, e.Level = day.Format("2006-01-02"), int(level)
	return e, nil
}
