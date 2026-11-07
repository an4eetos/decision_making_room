package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
)

const campaignColumns = `id, quarter, name, phase, note, review, review_at, opened_at, closed_at,
	calibration, calibration_n, created_at, updated_at`

const predictionColumns = `id, campaign_id, objective_id, text, warning, status, chance, outcome,
	scored_at, source, session_id, message_id, fingerprint, created_at, updated_at`

const commendationColumns = `id, campaign_id, kind, text, ref_id, created_at`

func (r *Repository) CreateCampaign(ctx context.Context, c domain.Campaign) (domain.Campaign, error) {
	created, err := scanCampaign(r.pool.QueryRow(ctx, `
		INSERT INTO campaigns (quarter, name, phase, opened_at)
		VALUES ($1, $2, $3, $4)
		RETURNING `+campaignColumns, c.Quarter, c.Name, string(c.Phase), c.OpenedAt))
	if isUniqueViolation(err) {
		return domain.Campaign{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("create campaign: %w", err)
	}
	return created, nil
}

func (r *Repository) GetCampaign(ctx context.Context, id uuid.UUID) (domain.Campaign, error) {
	c, err := scanCampaign(r.pool.QueryRow(ctx, `SELECT `+campaignColumns+` FROM campaigns WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("get campaign: %w", err)
	}
	return c, nil
}

func (r *Repository) UpdateCampaign(ctx context.Context, c domain.Campaign) (domain.Campaign, error) {
	updated, err := scanCampaign(r.pool.QueryRow(ctx, `
		UPDATE campaigns SET
			name = $2, phase = $3, note = $4, review = $5, review_at = $6, updated_at = now()
		WHERE id = $1 AND phase <> 'closed'
		RETURNING `+campaignColumns, c.ID, c.Name, string(c.Phase), c.Note, c.Review, c.ReviewAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("update campaign: %w", err)
	}
	return updated, nil
}

func (r *Repository) DeleteCampaign(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM campaigns WHERE id = $1 AND phase <> 'closed'`, id)
	if err != nil {
		return fmt.Errorf("delete campaign: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (r *Repository) ListCampaigns(ctx context.Context) ([]domain.Campaign, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+campaignColumns+` FROM campaigns ORDER BY quarter DESC`)
	if err != nil {
		return nil, fmt.Errorf("list campaigns: %w", err)
	}
	defer rows.Close()
	return collect(rows, scanCampaign)
}

func (r *Repository) AssignObjective(ctx context.Context, campaignID, itemID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO campaign_objectives (campaign_id, item_id) VALUES ($1, $2)`, campaignID, itemID)
	if isUniqueViolation(err) {
		return port.ErrDuplicate
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return port.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("assign objective: %w", err)
	}
	return nil
}

func (r *Repository) UnassignObjective(ctx context.Context, campaignID, itemID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM campaign_objectives WHERE campaign_id = $1 AND item_id = $2`, campaignID, itemID)
	if err != nil {
		return fmt.Errorf("unassign objective: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

func (r *Repository) ListAssignments(ctx context.Context, campaignID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT item_id FROM campaign_objectives WHERE campaign_id = $1`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("list assignments: %w", err)
	}
	defer rows.Close()

	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (r *Repository) CreatePrediction(ctx context.Context, p domain.Prediction) (domain.Prediction, error) {
	created, err := scanPrediction(r.pool.QueryRow(ctx, `
		INSERT INTO predictions
			(campaign_id, objective_id, text, warning, status, source, session_id, message_id, fingerprint)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+predictionColumns,
		p.CampaignID, p.ObjectiveID, p.Text, p.Warning, string(p.Status), p.Source, p.SessionID, p.MessageID, p.Fingerprint))
	if isUniqueViolation(err) {
		return domain.Prediction{}, port.ErrDuplicate
	}
	if err != nil {
		return domain.Prediction{}, fmt.Errorf("create prediction: %w", err)
	}
	return created, nil
}

func (r *Repository) GetPrediction(ctx context.Context, id uuid.UUID) (domain.Prediction, error) {
	p, err := scanPrediction(r.pool.QueryRow(ctx, `SELECT `+predictionColumns+` FROM predictions WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Prediction{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Prediction{}, fmt.Errorf("get prediction: %w", err)
	}
	return p, nil
}

func (r *Repository) UpdatePrediction(ctx context.Context, p domain.Prediction) (domain.Prediction, error) {
	updated, err := scanPrediction(r.pool.QueryRow(ctx, `
		UPDATE predictions SET
			campaign_id = $2, objective_id = $3, status = $4, chance = $5, outcome = $6,
			scored_at = $7, updated_at = now()
		WHERE id = $1
		RETURNING `+predictionColumns,
		p.ID, p.CampaignID, p.ObjectiveID, string(p.Status), string(p.Chance), string(p.Outcome), p.ScoredAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Prediction{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Prediction{}, fmt.Errorf("update prediction: %w", err)
	}
	return updated, nil
}

func (r *Repository) ListPredictions(ctx context.Context) ([]domain.Prediction, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+predictionColumns+` FROM predictions ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list predictions: %w", err)
	}
	defer rows.Close()
	return collect(rows, scanPrediction)
}

func (r *Repository) SealCampaign(ctx context.Context, c domain.Campaign, lines []domain.Commendation) (domain.Campaign, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("seal campaign: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sealed, err := scanCampaign(tx.QueryRow(ctx, `
		UPDATE campaigns SET
			phase = 'closed', closed_at = $2, calibration = $3, calibration_n = $4, updated_at = now()
		WHERE id = $1 AND phase <> 'closed'
		RETURNING `+campaignColumns, c.ID, c.ClosedAt, c.Calibration, c.CalibrationN))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, fmt.Errorf("seal campaign: %w", err)
	}
	for _, line := range lines {
		if _, err := tx.Exec(ctx, `
			INSERT INTO commendations (campaign_id, kind, text, ref_id, created_at)
			VALUES ($1, $2, $3, $4, $5)`,
			c.ID, string(line.Kind), line.Text, line.RefID, line.CreatedAt); err != nil {
			return domain.Campaign{}, fmt.Errorf("seal commendation: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Campaign{}, fmt.Errorf("seal campaign: %w", err)
	}
	return sealed, nil
}

func (r *Repository) ListCommendations(ctx context.Context, campaignID uuid.UUID) ([]domain.Commendation, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+commendationColumns+` FROM commendations
		WHERE campaign_id = $1 ORDER BY created_at, kind`, campaignID)
	if err != nil {
		return nil, fmt.Errorf("list commendations: %w", err)
	}
	defer rows.Close()
	return collect(rows, scanCommendation)
}

func collect[T any](rows pgx.Rows, scan func(scannable) (T, error)) ([]T, error) {
	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanCampaign(row scannable) (domain.Campaign, error) {
	var c domain.Campaign
	var phase string
	err := row.Scan(&c.ID, &c.Quarter, &c.Name, &phase, &c.Note, &c.Review, &c.ReviewAt,
		&c.OpenedAt, &c.ClosedAt, &c.Calibration, &c.CalibrationN, &c.CreatedAt, &c.UpdatedAt)
	c.Phase = domain.Phase(phase)
	return c, err
}

func scanPrediction(row scannable) (domain.Prediction, error) {
	var p domain.Prediction
	var status, chance, outcome string
	err := row.Scan(&p.ID, &p.CampaignID, &p.ObjectiveID, &p.Text, &p.Warning, &status, &chance, &outcome,
		&p.ScoredAt, &p.Source, &p.SessionID, &p.MessageID, &p.Fingerprint, &p.CreatedAt, &p.UpdatedAt)
	p.Status, p.Chance, p.Outcome = domain.PredictionStatus(status), domain.Chance(chance), domain.Outcome(outcome)
	return p, err
}

func scanCommendation(row scannable) (domain.Commendation, error) {
	var c domain.Commendation
	var kind string
	err := row.Scan(&c.ID, &c.CampaignID, &kind, &c.Text, &c.RefID, &c.CreatedAt)
	c.Kind = domain.CommendationKind(kind)
	return c, err
}
