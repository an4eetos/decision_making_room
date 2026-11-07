package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	memport "github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

// Price localises the catalogue's global anchor prices to a destination.
//
// The model is good at relative cost — that a fan in Bangkok is cheaper than one
// in Zurich — and bad at exact figures. So every line it touches is marked
// CostFromModel with a confidence, and the UI shows that. A budget that cannot
// tell you which numbers are guesses is worse than one with no numbers.
type Price struct {
	llm memport.LLM
}

func NewPrice(llm memport.LLM) *Price {
	return &Price{llm: llm}
}

const pricingSystemPrompt = `You estimate local retail prices for someone setting up a home in a specific city.

You are given a destination, a target currency, and a list of items with a rough
global anchor price in USD. Adjust each to what it would actually cost there,
buying at an ordinary supermarket, pharmacy or homeware shop — not a tourist
shop and not a luxury brand.

Return ONLY a JSON array, no prose and no code fences:
[{"id": "<item id>", "unit_cost": <number in the target currency>, "confidence": <0.0-1.0>}]

Rules:
- Use the target currency, not USD.
- confidence reflects how sure you are for THIS city: 0.8 for a common
  supermarket item in a city you know well, 0.3 for something variable like a
  co-working membership or a chair.
- Keep an anchor of 0 at 0. Those are checks and documents, not purchases.
- Omit any item you cannot estimate. Omitting is better than inventing.`

// Apply prices items in place. An error here is never fatal to the plan: the
// anchors are already set, so a failed or unparseable pricing pass degrades to
// them rather than losing the plan.
func (p *Price) Apply(ctx context.Context, plan domain.Plan, items []domain.Item) error {
	if p == nil || p.llm == nil || len(items) == 0 {
		return nil
	}

	priceable := make([]*domain.Item, 0, len(items))
	for i := range items {
		// Never overwrite a price that came from reality.
		if items[i].Source.Trustworthy() {
			continue
		}
		if items[i].UnitCost != nil && *items[i].UnitCost == 0 {
			continue
		}
		priceable = append(priceable, &items[i])
	}
	if len(priceable) == 0 {
		return nil
	}

	currency := plan.Currency
	if currency == "" {
		currency = "USD"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Destination: %s\nTarget currency: %s\nTypical stay: %d nights\n\nItems:\n",
		plan.Destination, currency, plan.Nights)
	for _, item := range priceable {
		anchor := 0.0
		if item.UnitCost != nil {
			anchor = *item.UnitCost
		}
		fmt.Fprintf(&b, "- id=%s | %s | unit=%s | anchor_usd=%.0f\n",
			item.CatalogID, item.Name, item.Unit, anchor)
	}

	answer, err := p.llm.Chat(ctx, []memport.Message{
		{Role: "system", Content: pricingSystemPrompt},
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		log.Printf("relocation: pricing call failed, keeping anchor prices: %v", err)
		return nil
	}

	quotes, err := parsePriceQuotes(answer)
	if err != nil {
		log.Printf("relocation: could not parse pricing response, keeping anchor prices: %v", err)
		return nil
	}

	byID := make(map[string]*domain.Item, len(priceable))
	for _, item := range priceable {
		byID[item.CatalogID] = item
	}

	for _, q := range quotes {
		item, ok := byID[q.ID]
		if !ok || q.UnitCost < 0 {
			continue
		}
		cost := q.UnitCost
		confidence := clamp01(q.Confidence)
		item.UnitCost = &cost
		item.Currency = currency
		item.Source = domain.CostFromModel
		item.Confidence = &confidence
	}

	return nil
}

type priceQuote struct {
	ID         string  `json:"id"`
	UnitCost   float64 `json:"unit_cost"`
	Confidence float64 `json:"confidence"`
}

// parsePriceQuotes tolerates the code fences and leading prose that models add
// despite being told not to.
func parsePriceQuotes(answer string) ([]priceQuote, error) {
	raw := strings.TrimSpace(answer)
	if fence := strings.Index(raw, "```"); fence >= 0 {
		raw = raw[fence+3:]
		if nl := strings.IndexByte(raw, '\n'); nl >= 0 {
			raw = raw[nl+1:]
		}
		if end := strings.Index(raw, "```"); end >= 0 {
			raw = raw[:end]
		}
	}

	start := strings.IndexByte(raw, '[')
	end := strings.LastIndexByte(raw, ']')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array in response")
	}

	var quotes []priceQuote
	if err := json.Unmarshal([]byte(raw[start:end+1]), &quotes); err != nil {
		return nil, err
	}
	return quotes, nil
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
