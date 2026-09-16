package relocationapi

import (
	"slices"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

type planSummary struct {
	ID          uuid.UUID `json:"id"`
	Destination string    `json:"destination"`
	Nights      int       `json:"nights"`
	ArriveOn    string    `json:"arrive_on,omitempty"`
	DepartOn    string    `json:"depart_on,omitempty"`
	Status      string    `json:"status"`
	Currency    string    `json:"currency"`
}

type itemResponse struct {
	ID                uuid.UUID `json:"id"`
	CatalogID         string    `json:"catalog_id,omitempty"`
	Category          string    `json:"category"`
	Name              string    `json:"name"`
	Quantity          float64   `json:"quantity"`
	Unit              string    `json:"unit,omitempty"`
	UnitCost          *float64  `json:"unit_cost"`
	LineTotal         *float64  `json:"line_total"`
	Currency          string    `json:"currency"`
	Source            string    `json:"cost_source"`
	Confidence        *float64  `json:"confidence,omitempty"`
	Estimated         bool      `json:"estimated"`
	Status            string    `json:"status"`
	Note              string    `json:"note,omitempty"`
	CommonlyForgotten bool      `json:"commonly_forgotten"`
}

type pitfallResponse struct {
	ID           uuid.UUID `json:"id"`
	PitfallID    string    `json:"pitfall_id"`
	Title        string    `json:"title"`
	Body         string    `json:"body"`
	Severity     string    `json:"severity"`
	Action       string    `json:"action"`
	Acknowledged bool      `json:"acknowledged"`
}

// categoryTotal is what you will spend on one part of setting up.
type categoryTotal struct {
	Category string  `json:"category"`
	Total    float64 `json:"total"`
	Items    int     `json:"items"`
	Unpriced int     `json:"unpriced"`
}

// budget deliberately separates what is known from what is guessed. EstimatedShare
// is the fraction of the total that came from model estimates rather than from a
// price you confirmed or previously paid — a budget that cannot tell you how much
// of itself is a guess is worse than one with no numbers.
type budget struct {
	Currency       string  `json:"currency"`
	Total          float64 `json:"total"`
	EstimatedTotal float64 `json:"estimated_total"`
	ConfirmedTotal float64 `json:"confirmed_total"`
	EstimatedShare float64 `json:"estimated_share"`
	UnpricedItems  int     `json:"unpriced_items"`
	// Mixed is true when lines are priced in more than one currency, which is the
	// state between creating a plan and repricing it. OtherCurrencies names the
	// ones left out of the total rather than letting them vanish.
	Mixed           bool            `json:"mixed_currencies"`
	OtherCurrencies []string        `json:"other_currencies,omitempty"`
	ItemsNeeded     int             `json:"items_needed"`
	ItemsResolved   int             `json:"items_resolved"`
	AlreadyCovered  float64         `json:"already_covered"`
	ByCategory      []categoryTotal `json:"by_category"`
}

type planResponse struct {
	planSummary
	CountryCode string            `json:"country_code,omitempty"`
	PartySize   int               `json:"party_size"`
	Housing     string            `json:"housing,omitempty"`
	Climate     string            `json:"climate,omitempty"`
	BudgetStyle string            `json:"budget_style"`
	Notes       string            `json:"notes,omitempty"`
	Budget      budget            `json:"budget"`
	Items       []itemResponse    `json:"items"`
	Pitfalls    []pitfallResponse `json:"pitfalls"`
}

func toItemResponse(item domain.Item) itemResponse {
	return itemResponse{
		ID:                item.ID,
		CatalogID:         item.CatalogID,
		Category:          string(item.Category),
		Name:              item.Name,
		Quantity:          item.Quantity,
		Unit:              item.Unit,
		UnitCost:          item.UnitCost,
		LineTotal:         item.LineTotal(),
		Currency:          item.Currency,
		Source:            string(item.Source),
		Confidence:        item.Confidence,
		Estimated:         !item.Source.Trustworthy(),
		Status:            string(item.Status),
		Note:              item.Note,
		CommonlyForgotten: item.CommonlyForgotten,
	}
}

func toPlanResponse(plan domain.Plan) planResponse {
	items := make([]itemResponse, 0, len(plan.Items))
	for _, item := range plan.Items {
		items = append(items, toItemResponse(item))
	}

	pitfalls := make([]pitfallResponse, 0, len(plan.Pitfalls))
	for _, p := range plan.Pitfalls {
		pitfalls = append(pitfalls, pitfallResponse{
			ID:           p.ID,
			PitfallID:    p.PitfallID,
			Title:        p.Title,
			Body:         p.Body,
			Severity:     string(p.Severity),
			Action:       p.Action,
			Acknowledged: p.AcknowledgedAt != nil,
		})
	}

	return planResponse{
		planSummary: planSummary{
			ID:          plan.ID,
			Destination: plan.Destination,
			Nights:      plan.Nights,
			ArriveOn:    formatDate(plan.ArriveOn),
			DepartOn:    formatDate(plan.DepartOn),
			Status:      string(plan.Status),
			Currency:    plan.Currency,
		},
		CountryCode: plan.CountryCode,
		PartySize:   plan.PartySize,
		Housing:     string(plan.Housing),
		Climate:     string(plan.Climate),
		BudgetStyle: string(plan.BudgetStyle),
		Notes:       plan.Notes,
		Budget:      computeBudget(plan),
		Items:       items,
		Pitfalls:    pitfalls,
	}
}

func computeBudget(plan domain.Plan) budget {
	// Report in whichever currency the priced lines are actually in, rather than
	// stamping the plan's currency onto USD anchors. Before a pricing pass that
	// means USD; after one it means the plan currency.
	reporting, others := reportingCurrency(plan.Items)
	if reporting == "" {
		reporting = plan.Currency
	}

	b := budget{Currency: reporting, Mixed: len(others) > 0, OtherCurrencies: others}

	byCategory := make(map[domain.Category]*categoryTotal)
	order := make([]domain.Category, 0, 8)

	for _, item := range plan.Items {
		cat, ok := byCategory[item.Category]
		if !ok {
			cat = &categoryTotal{Category: string(item.Category)}
			byCategory[item.Category] = cat
			order = append(order, item.Category)
		}
		cat.Items++

		switch item.Status {
		case domain.ItemHave, domain.ItemSkipped:
			// Not spending on it. Count what it would have cost so the list can
			// show what carrying your own things is worth.
			b.ItemsResolved++
			if item.UnitCost != nil && item.Currency == reporting {
				b.AlreadyCovered += *item.UnitCost * item.Quantity
			}
			continue
		case domain.ItemBought:
			b.ItemsResolved++
		default:
			b.ItemsNeeded++
		}

		// Lines in another currency are excluded rather than summed blindly; the
		// count surfaces in OtherCurrencies so they are not silently dropped.
		if item.Currency != reporting && item.UnitCost != nil {
			cat.Unpriced++
			b.UnpricedItems++
			continue
		}

		total := item.LineTotal()
		if total == nil {
			// An unknown price contributes nothing to the total and is counted
			// separately, so it cannot silently read as free.
			b.UnpricedItems++
			cat.Unpriced++
			continue
		}

		b.Total += *total
		cat.Total += *total
		if item.Source.Trustworthy() {
			b.ConfirmedTotal += *total
		} else {
			b.EstimatedTotal += *total
		}
	}

	if b.Total > 0 {
		b.EstimatedShare = b.EstimatedTotal / b.Total
	}

	b.ByCategory = make([]categoryTotal, 0, len(order))
	for _, cat := range order {
		b.ByCategory = append(b.ByCategory, *byCategory[cat])
	}
	return b
}

// reportingCurrency picks the currency carrying the largest priced total, and
// returns the others so the response can say what it left out.
func reportingCurrency(items []domain.Item) (string, []string) {
	totals := make(map[string]float64)
	for _, item := range items {
		if item.UnitCost == nil || item.Currency == "" {
			continue
		}
		totals[item.Currency] += *item.UnitCost * item.Quantity
	}
	if len(totals) == 0 {
		return "", nil
	}

	best := ""
	for currency, total := range totals {
		// Ties break on the currency code so the choice is stable between calls.
		if best == "" || total > totals[best] || (total == totals[best] && currency < best) {
			best = currency
		}
	}

	others := make([]string, 0, len(totals)-1)
	for currency := range totals {
		if currency != best {
			others = append(others, currency)
		}
	}
	slices.Sort(others)
	return best, others
}
