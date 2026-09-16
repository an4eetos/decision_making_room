package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

const planColumns = `
	SELECT id, destination, country_code, arrive_on, depart_on, nights, party_size,
	       housing, climate, budget_style, currency, status, notes, created_at, updated_at
	FROM relocation_plans`

type scannable interface {
	Scan(dest ...any) error
}

func (r *Repository) scanPlan(row scannable) (domain.Plan, error) {
	var (
		p                        domain.Plan
		housing, climate, budget string
		status                   string
		arriveOn, departOn       *time.Time
	)

	err := row.Scan(&p.ID, &p.Destination, &p.CountryCode, &arriveOn, &departOn,
		&p.Nights, &p.PartySize, &housing, &climate, &budget, &p.Currency,
		&status, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.Plan{}, err
	}

	p.Housing = domain.Housing(housing)
	p.Climate = domain.Climate(climate)
	p.BudgetStyle = domain.BudgetStyle(budget)
	p.Status = domain.PlanStatus(status)
	if arriveOn != nil {
		p.ArriveOn = *arriveOn
	}
	if departOn != nil {
		p.DepartOn = *departOn
	}
	return p, nil
}

const itemColumns = `
	SELECT id, plan_id, catalog_id, category, name, quantity, unit, unit_cost,
	       currency, cost_source, confidence, status, note, commonly_forgotten,
	       sort_order, created_at, updated_at
	FROM relocation_items`

func scanItem(row scannable) (domain.Item, error) {
	var (
		item                   domain.Item
		category, source, stat string
	)

	err := row.Scan(&item.ID, &item.PlanID, &item.CatalogID, &category, &item.Name,
		&item.Quantity, &item.Unit, &item.UnitCost, &item.Currency, &source,
		&item.Confidence, &stat, &item.Note, &item.CommonlyForgotten,
		&item.SortOrder, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.Item{}, err
	}

	item.Category = domain.Category(category)
	item.Source = domain.CostSource(source)
	item.Status = domain.ItemStatus(stat)
	return item, nil
}

func (r *Repository) items(ctx context.Context, planID uuid.UUID) ([]domain.Item, error) {
	rows, err := r.pool.Query(ctx, itemColumns+` WHERE plan_id = $1 ORDER BY sort_order, name`, planID)
	if err != nil {
		return nil, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()

	var items []domain.Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("scan item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) pitfalls(ctx context.Context, planID uuid.UUID) ([]domain.PlanPitfall, error) {
	const q = `
		SELECT id, plan_id, pitfall_id, title, body, severity, action, acknowledged_at, created_at
		FROM relocation_pitfalls
		WHERE plan_id = $1
		ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'costly' THEN 1 ELSE 2 END, title`

	rows, err := r.pool.Query(ctx, q, planID)
	if err != nil {
		return nil, fmt.Errorf("list pitfalls: %w", err)
	}
	defer rows.Close()

	var out []domain.PlanPitfall
	for rows.Next() {
		var (
			p        domain.PlanPitfall
			severity string
		)
		if err := rows.Scan(&p.ID, &p.PlanID, &p.PitfallID, &p.Title, &p.Body,
			&severity, &p.Action, &p.AcknowledgedAt, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pitfall: %w", err)
		}
		p.Severity = domain.Severity(severity)
		out = append(out, p)
	}
	return out, rows.Err()
}

// nullDate keeps an unset date NULL rather than storing year zero.
func nullDate(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
