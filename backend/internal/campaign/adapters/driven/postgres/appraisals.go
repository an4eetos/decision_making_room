package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

const appraisalColumns = `id, front_id, tier, generals, body, session_id, created_at`

func (r *Repository) CreateAppraisal(ctx context.Context, a domain.Appraisal) (domain.Appraisal, error) {
	generals := a.Generals
	if generals == nil {
		generals = []string{}
	}
	created, err := scanAppraisal(r.pool.QueryRow(ctx, `
		INSERT INTO campaign_appraisals (front_id, tier, generals, body)
		VALUES ($1, $2, $3, $4)
		RETURNING `+appraisalColumns, a.FrontID, a.Tier, generals, a.Body))
	if err != nil {
		return domain.Appraisal{}, fmt.Errorf("create appraisal: %w", err)
	}
	return created, nil
}

func (r *Repository) GetAppraisal(ctx context.Context, id uuid.UUID) (domain.Appraisal, error) {
	a, err := scanAppraisal(r.pool.QueryRow(ctx, `SELECT `+appraisalColumns+` FROM campaign_appraisals WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Appraisal{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Appraisal{}, fmt.Errorf("get appraisal: %w", err)
	}
	return a, nil
}

func (r *Repository) ListAppraisals(ctx context.Context, limit int) ([]domain.Appraisal, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+appraisalColumns+` FROM campaign_appraisals
		ORDER BY created_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list appraisals: %w", err)
	}
	defer rows.Close()

	var out []domain.Appraisal
	for rows.Next() {
		a, err := scanAppraisal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) AttachAppraisalSession(ctx context.Context, id, sessionID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE campaign_appraisals SET session_id = $2 WHERE id = $1`, id, sessionID)
	if err != nil {
		return fmt.Errorf("attach appraisal session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

func scanAppraisal(row scannable) (domain.Appraisal, error) {
	var a domain.Appraisal
	if err := row.Scan(&a.ID, &a.FrontID, &a.Tier, &a.Generals, &a.Body, &a.SessionID, &a.CreatedAt); err != nil {
		return domain.Appraisal{}, err
	}
	if a.Generals == nil {
		a.Generals = []string{}
	}
	return a, nil
}
