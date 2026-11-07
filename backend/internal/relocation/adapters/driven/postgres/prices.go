package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/an4eetos/decision-room/internal/relocation/port"
)

// PriceRepository remembers what you actually paid, per catalogue item per
// destination.
type PriceRepository struct {
	pool *pgxpool.Pool
}

func NewPriceRepository(pool *pgxpool.Pool) *PriceRepository { return &PriceRepository{pool: pool} }

func (r *PriceRepository) Record(ctx context.Context, catalogID, destination string, unitCost float64, currency string) error {
	if catalogID == "" || destination == "" {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO price_observations (catalog_id, destination, unit_cost, currency) VALUES ($1,$2,$3,$4)`,
		catalogID, destination, unitCost, currency)
	if err != nil {
		return fmt.Errorf("record price: %w", err)
	}
	return nil
}

// Lookup returns the most recent observation per catalogue id. DISTINCT ON does
// the "latest per group" in one pass rather than a window function plus filter.
func (r *PriceRepository) Lookup(ctx context.Context, destination string, catalogIDs []string) (map[string]port.Observation, error) {
	if destination == "" || len(catalogIDs) == 0 {
		return nil, nil
	}

	const q = `
		SELECT DISTINCT ON (catalog_id) catalog_id, unit_cost, currency
		FROM price_observations
		WHERE lower(destination) = lower($1) AND catalog_id = ANY($2)
		ORDER BY catalog_id, observed_at DESC`

	rows, err := r.pool.Query(ctx, q, destination, catalogIDs)
	if err != nil {
		return nil, fmt.Errorf("lookup prices: %w", err)
	}
	defer rows.Close()

	out := make(map[string]port.Observation)
	for rows.Next() {
		var (
			id  string
			obs port.Observation
		)
		if err := rows.Scan(&id, &obs.UnitCost, &obs.Currency); err != nil {
			return nil, fmt.Errorf("scan price: %w", err)
		}
		out[id] = obs
	}
	return out, rows.Err()
}
