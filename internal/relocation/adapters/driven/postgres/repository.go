// Package postgres persists relocation plans.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/port"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) CreatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	const q = `
		INSERT INTO relocation_plans
			(destination, country_code, arrive_on, depart_on, nights, party_size,
			 housing, climate, budget_style, currency, status, notes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING id`

	var id uuid.UUID
	err := r.pool.QueryRow(ctx, q,
		p.Destination, p.CountryCode, nullDate(p.ArriveOn), nullDate(p.DepartOn),
		p.Nights, p.PartySize, string(p.Housing), string(p.Climate),
		string(p.BudgetStyle), p.Currency, string(p.Status), p.Notes,
	).Scan(&id)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("create plan: %w", err)
	}
	return r.GetPlan(ctx, id)
}

func (r *Repository) UpdatePlan(ctx context.Context, p domain.Plan) (domain.Plan, error) {
	const q = `
		UPDATE relocation_plans SET
			destination = $2, country_code = $3, arrive_on = $4, depart_on = $5,
			nights = $6, party_size = $7, housing = $8, climate = $9,
			budget_style = $10, currency = $11, status = $12, notes = $13,
			updated_at = now()
		WHERE id = $1
		RETURNING id`

	var id uuid.UUID
	err := r.pool.QueryRow(ctx, q, p.ID,
		p.Destination, p.CountryCode, nullDate(p.ArriveOn), nullDate(p.DepartOn),
		p.Nights, p.PartySize, string(p.Housing), string(p.Climate),
		string(p.BudgetStyle), p.Currency, string(p.Status), p.Notes,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Plan{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Plan{}, fmt.Errorf("update plan: %w", err)
	}
	return r.GetPlan(ctx, id)
}

func (r *Repository) GetPlan(ctx context.Context, id uuid.UUID) (domain.Plan, error) {
	plan, err := r.scanPlan(r.pool.QueryRow(ctx, planColumns+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Plan{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Plan{}, fmt.Errorf("get plan: %w", err)
	}

	if plan.Items, err = r.items(ctx, id); err != nil {
		return domain.Plan{}, err
	}
	if plan.Pitfalls, err = r.pitfalls(ctx, id); err != nil {
		return domain.Plan{}, err
	}
	return plan, nil
}

func (r *Repository) ListPlans(ctx context.Context) ([]domain.Plan, error) {
	rows, err := r.pool.Query(ctx, planColumns+` ORDER BY COALESCE(arrive_on, created_at::date) DESC, created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()

	var plans []domain.Plan
	for rows.Next() {
		plan, err := r.scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, rows.Err()
}

func (r *Repository) DeletePlan(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM relocation_plans WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

// ReplaceItems upserts catalogue lines and deletes the ones that no longer
// apply. It runs in a transaction so a rebuild is all-or-nothing.
//
// The ON CONFLICT clause deliberately leaves status, unit_cost, cost_source,
// confidence and note alone when the price came from you: changing a departure
// date would otherwise wipe every item you had already ticked off or priced.
func (r *Repository) ReplaceItems(ctx context.Context, planID uuid.UUID, items []domain.Item) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	keep := make([]string, 0, len(items))
	for _, item := range items {
		keep = append(keep, item.CatalogID)

		const q = `
			INSERT INTO relocation_items
				(plan_id, catalog_id, category, name, quantity, unit, unit_cost,
				 currency, cost_source, confidence, status, note, commonly_forgotten, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
			ON CONFLICT (plan_id, catalog_id) WHERE catalog_id <> '' DO UPDATE SET
				category           = EXCLUDED.category,
				name               = EXCLUDED.name,
				quantity           = EXCLUDED.quantity,
				unit               = EXCLUDED.unit,
				note               = EXCLUDED.note,
				commonly_forgotten = EXCLUDED.commonly_forgotten,
				sort_order         = EXCLUDED.sort_order,
				unit_cost   = CASE WHEN relocation_items.cost_source IN ('user','history')
				                   THEN relocation_items.unit_cost ELSE EXCLUDED.unit_cost END,
				currency    = CASE WHEN relocation_items.cost_source IN ('user','history')
				                   THEN relocation_items.currency ELSE EXCLUDED.currency END,
				cost_source = CASE WHEN relocation_items.cost_source IN ('user','history')
				                   THEN relocation_items.cost_source ELSE EXCLUDED.cost_source END,
				confidence  = CASE WHEN relocation_items.cost_source IN ('user','history')
				                   THEN relocation_items.confidence ELSE EXCLUDED.confidence END,
				updated_at  = now()`

		if _, err := tx.Exec(ctx, q,
			planID, item.CatalogID, string(item.Category), item.Name, item.Quantity,
			item.Unit, item.UnitCost, item.Currency, string(item.Source), item.Confidence,
			string(item.Status), item.Note, item.CommonlyForgotten, item.SortOrder,
		); err != nil {
			return fmt.Errorf("upsert item %q: %w", item.CatalogID, err)
		}
	}

	// Manual lines (empty catalog_id) are never swept: you added them on purpose.
	if _, err := tx.Exec(ctx,
		`DELETE FROM relocation_items
		 WHERE plan_id = $1 AND catalog_id <> '' AND NOT (catalog_id = ANY($2))`,
		planID, keep,
	); err != nil {
		return fmt.Errorf("prune items: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *Repository) AddItem(ctx context.Context, item domain.Item) (domain.Item, error) {
	const q = `
		INSERT INTO relocation_items
			(plan_id, catalog_id, category, name, quantity, unit, unit_cost,
			 currency, cost_source, status, note, sort_order)
		VALUES ($1,'',$2,$3,$4,$5,$6,$7,$8,$9,$10,
			COALESCE((SELECT MAX(sort_order) + 1 FROM relocation_items WHERE plan_id = $1), 0))
		RETURNING id`

	var id uuid.UUID
	err := r.pool.QueryRow(ctx, q,
		item.PlanID, string(item.Category), item.Name, item.Quantity, item.Unit,
		item.UnitCost, item.Currency, string(item.Source), string(item.Status), item.Note,
	).Scan(&id)
	if err != nil {
		return domain.Item{}, fmt.Errorf("add item: %w", err)
	}

	item.ID = id
	return item, nil
}

func (r *Repository) GetItem(ctx context.Context, id uuid.UUID) (domain.Item, error) {
	item, err := scanItem(r.pool.QueryRow(ctx, itemColumns+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("get item: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error) {
	const q = `
		UPDATE relocation_items SET
			name = $2, quantity = $3, unit = $4, unit_cost = $5, currency = $6,
			cost_source = $7, confidence = $8, status = $9, note = $10, updated_at = now()
		WHERE id = $1
		RETURNING id, plan_id, catalog_id, category, name, quantity, unit, unit_cost,
		          currency, cost_source, confidence, status, note, commonly_forgotten,
		          sort_order, created_at, updated_at`

	item, err := scanItem(r.pool.QueryRow(ctx, q,
		item.ID, item.Name, item.Quantity, item.Unit, item.UnitCost, item.Currency,
		string(item.Source), item.Confidence, string(item.Status), item.Note))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Item{}, port.ErrNotFound
	}
	if err != nil {
		return domain.Item{}, fmt.Errorf("update item: %w", err)
	}
	return item, nil
}

func (r *Repository) DeleteItem(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM relocation_items WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}

// ReplacePitfalls preserves acknowledgements: a warning you have already dealt
// with should not reappear unacknowledged because you changed the dates.
func (r *Repository) ReplacePitfalls(ctx context.Context, planID uuid.UUID, pitfalls []domain.PlanPitfall) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	keep := make([]string, 0, len(pitfalls))
	for _, p := range pitfalls {
		keep = append(keep, p.PitfallID)

		const q = `
			INSERT INTO relocation_pitfalls (plan_id, pitfall_id, title, body, severity, action)
			VALUES ($1,$2,$3,$4,$5,$6)
			ON CONFLICT (plan_id, pitfall_id) DO UPDATE SET
				title = EXCLUDED.title, body = EXCLUDED.body,
				severity = EXCLUDED.severity, action = EXCLUDED.action`

		if _, err := tx.Exec(ctx, q, planID, p.PitfallID, p.Title, p.Body, string(p.Severity), p.Action); err != nil {
			return fmt.Errorf("upsert pitfall %q: %w", p.PitfallID, err)
		}
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM relocation_pitfalls WHERE plan_id = $1 AND NOT (pitfall_id = ANY($2))`,
		planID, keep,
	); err != nil {
		return fmt.Errorf("prune pitfalls: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *Repository) AcknowledgePitfall(ctx context.Context, id uuid.UUID, acknowledged bool) error {
	var at any
	if acknowledged {
		at = time.Now().UTC()
	}
	tag, err := r.pool.Exec(ctx, `UPDATE relocation_pitfalls SET acknowledged_at = $2 WHERE id = $1`, id, at)
	if err != nil {
		return fmt.Errorf("acknowledge pitfall: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrNotFound
	}
	return nil
}
