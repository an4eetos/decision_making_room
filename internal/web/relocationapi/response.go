package relocationapi

import (
	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/service"
)

type planSummary struct {
	ID          uuid.UUID `json:"id"`
	Destination string    `json:"destination"`
	Nights      int       `json:"nights"`
	ArriveOn    string    `json:"arrive_on,omitempty"`
	DepartOn    string    `json:"depart_on,omitempty"`
	Status      string    `json:"status"`
	Currency    string    `json:"currency"`
	// FrontID is the campaign front the stay is deployed on, if any.
	FrontID *uuid.UUID `json:"front_id"`
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

// budget is the plan's budget as the planner computes it; the arithmetic
// lives in the service so a deployment on the campaign map reads the same
// numbers this page shows.
type budget = service.Budget

type planResponse struct {
	planSummary
	CountryCode string            `json:"country_code,omitempty"`
	PartySize   int               `json:"party_size"`
	Housing     string            `json:"housing,omitempty"`
	Climate     string            `json:"climate,omitempty"`
	BudgetStyle string            `json:"budget_style"`
	Notes       string            `json:"notes,omitempty"`
	Budget      budget            `json:"budget"`
	Comfort     comfortResponse   `json:"comfort"`
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

func (h *Handler) planResponse(plan domain.Plan) planResponse {
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
			FrontID:     plan.FrontID,
		},
		CountryCode: plan.CountryCode,
		PartySize:   plan.PartySize,
		Housing:     string(plan.Housing),
		Climate:     string(plan.Climate),
		BudgetStyle: string(plan.BudgetStyle),
		Notes:       plan.Notes,
		Budget:      service.ComputeBudget(plan),
		Comfort:     toComfortResponse(service.Comfort(plan.Items, h.comfort), h.comfort),
		Items:       items,
		Pitfalls:    pitfalls,
	}
}

// comfortResponse is the deterministic comfort reading. Nothing in it comes from
// a model, so unlike prices it carries no estimate flag; the radar's costs do,
// because they are line prices.
type comfortResponse struct {
	Score        *int               `json:"score"`
	Label        string             `json:"label,omitempty"`
	Arrival      *int               `json:"arrival"`
	ArrivalLabel string             `json:"arrival_label,omitempty"`
	Dimensions   []comfortDimension `json:"dimensions"`
	Radar        []comfortRadarStep `json:"radar"`
}

type comfortDimension struct {
	Dimension string        `json:"dimension"`
	Score     int           `json:"score"`
	Missing   []comfortItem `json:"missing"`
}

type comfortItem struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Weight string    `json:"weight"`
}

type comfortRadarStep struct {
	comfortItem
	Dimension  string   `json:"dimension"`
	ScoreAfter int      `json:"score_after"`
	Cost       *float64 `json:"cost"`
	Currency   string   `json:"currency"`
	Estimated  bool     `json:"estimated"`
}

func toComfortResponse(r service.ComfortReport, rules map[string]domain.ComfortRule) comfortResponse {
	out := comfortResponse{
		Score:        r.Score,
		Label:        r.Label,
		Arrival:      r.Arrival,
		ArrivalLabel: r.ArrivalLabel,
		Dimensions:   make([]comfortDimension, 0, len(r.Dimensions)),
		Radar:        make([]comfortRadarStep, 0, len(r.Radar)),
	}

	for _, d := range r.Dimensions {
		dim := comfortDimension{
			Dimension: string(d.Dimension),
			Score:     d.Score,
			Missing:   make([]comfortItem, 0, len(d.Missing)),
		}
		for _, item := range d.Missing {
			dim.Missing = append(dim.Missing, comfortItem{
				ID:     item.ID,
				Name:   item.Name,
				Weight: string(rules[item.CatalogID].Weight),
			})
		}
		out.Dimensions = append(out.Dimensions, dim)
	}

	for _, step := range r.Radar {
		out.Radar = append(out.Radar, comfortRadarStep{
			comfortItem: comfortItem{ID: step.Item.ID, Name: step.Item.Name, Weight: string(step.Weight)},
			Dimension:   string(step.Dimension),
			ScoreAfter:  step.ScoreAfter,
			Cost:        step.Item.LineTotal(),
			Currency:    step.Item.Currency,
			Estimated:   !step.Item.Source.Trustworthy(),
		})
	}
	return out
}
