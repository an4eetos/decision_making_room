package usecase

import (
	"context"
	"errors"
	"testing"

	memport "github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

type stubLLM struct {
	answer string
	err    error
}

func (s stubLLM) Chat(context.Context, []memport.Message) (string, error) {
	return s.answer, s.err
}

func items() []domain.Item {
	anchor := 9.0
	free := 0.0
	return []domain.Item{
		{CatalogID: "bath_towel", Name: "Bath towel", UnitCost: &anchor, Source: domain.CostFromCatalog},
		{CatalogID: "visa_check", Name: "Visa check", UnitCost: &free, Source: domain.CostFromCatalog},
	}
}

func TestPriceAppliesQuotesWithConfidence(t *testing.T) {
	t.Parallel()

	p := NewPrice(stubLLM{answer: `[{"id":"bath_towel","unit_cost":120,"confidence":0.7}]`})
	list := items()

	if err := p.Apply(context.Background(), domain.Plan{Destination: "Tbilisi", Currency: "GEL", Nights: 90}, list); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if *list[0].UnitCost != 120 || list[0].Currency != "GEL" {
		t.Fatalf("price not applied: %+v", list[0])
	}
	if list[0].Source != domain.CostFromModel {
		t.Fatalf("source = %q, want model", list[0].Source)
	}
	if list[0].Confidence == nil || *list[0].Confidence != 0.7 {
		t.Fatalf("confidence not recorded: %+v", list[0].Confidence)
	}
}

// Zero-anchor lines are checks and documents. Letting the model put a price on
// "confirm your visa type" would be nonsense in the budget.
func TestPriceLeavesZeroAnchorsAlone(t *testing.T) {
	t.Parallel()

	p := NewPrice(stubLLM{answer: `[{"id":"visa_check","unit_cost":50,"confidence":0.9}]`})
	list := items()

	if err := p.Apply(context.Background(), domain.Plan{Currency: "USD"}, list); err != nil {
		t.Fatal(err)
	}
	if *list[1].UnitCost != 0 {
		t.Fatalf("a zero-cost check was priced at %v", *list[1].UnitCost)
	}
}

// A price you confirmed must never be overwritten by an estimate.
func TestPriceNeverOverwritesTrustworthySources(t *testing.T) {
	t.Parallel()

	for _, source := range []domain.CostSource{domain.CostFromUser, domain.CostFromHistory} {
		known := 42.0
		list := []domain.Item{{CatalogID: "bath_towel", UnitCost: &known, Source: source}}

		p := NewPrice(stubLLM{answer: `[{"id":"bath_towel","unit_cost":999,"confidence":0.9}]`})
		if err := p.Apply(context.Background(), domain.Plan{Currency: "USD"}, list); err != nil {
			t.Fatal(err)
		}
		if *list[0].UnitCost != 42 {
			t.Fatalf("%s price was overwritten with %v", source, *list[0].UnitCost)
		}
	}
}

// A failed or unparseable pricing pass must degrade to the anchors, never lose
// the plan.
func TestPriceDegradesToAnchorsOnFailure(t *testing.T) {
	t.Parallel()

	cases := map[string]stubLLM{
		"call failed":    {err: errors.New("429 rate limited")},
		"prose not json": {answer: "Sure! Here are my estimates for Tbilisi..."},
		"malformed json": {answer: `[{"id":"bath_towel","unit_cost":`},
		"empty response": {answer: ""},
	}

	for name, llm := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			list := items()
			if err := NewPrice(llm).Apply(context.Background(), domain.Plan{Currency: "USD"}, list); err != nil {
				t.Fatalf("Apply returned an error instead of degrading: %v", err)
			}
			if *list[0].UnitCost != 9 || list[0].Source != domain.CostFromCatalog {
				t.Fatalf("anchor not preserved: %+v", list[0])
			}
		})
	}
}

// Models wrap JSON in code fences and prose no matter what the prompt says.
func TestParsePriceQuotesToleratesFencesAndProse(t *testing.T) {
	t.Parallel()

	answer := "Here you go:\n```json\n[{\"id\":\"a\",\"unit_cost\":5,\"confidence\":0.5}]\n```\nHope that helps!"
	quotes, err := parsePriceQuotes(answer)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(quotes) != 1 || quotes[0].ID != "a" {
		t.Fatalf("got %+v", quotes)
	}
}

func TestPriceClampsConfidence(t *testing.T) {
	t.Parallel()

	p := NewPrice(stubLLM{answer: `[{"id":"bath_towel","unit_cost":10,"confidence":7.5}]`})
	list := items()
	if err := p.Apply(context.Background(), domain.Plan{Currency: "USD"}, list); err != nil {
		t.Fatal(err)
	}
	if *list[0].Confidence != 1 {
		t.Fatalf("confidence = %v, want clamped to 1", *list[0].Confidence)
	}
}
