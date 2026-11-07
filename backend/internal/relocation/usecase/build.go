// Package usecase turns a stay into a plan: which items apply, how many, what
// they probably cost, and which mistakes are worth warning about.
package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/port"
	"github.com/an4eetos/decision-room/internal/relocation/service"
)

type Build struct {
	plans   port.PlanRepository
	prices  port.PriceRepository
	catalog port.CatalogReader
	pricer  *Price // optional; nil disables model pricing
}

func NewBuild(plans port.PlanRepository, prices port.PriceRepository, catalog port.CatalogReader, pricer *Price) *Build {
	return &Build{plans: plans, prices: prices, catalog: catalog, pricer: pricer}
}

type BuildInput struct {
	PlanID uuid.UUID
	// Reprice runs the model pricing pass. It is opt-in per rebuild because it
	// costs a model call and because estimates should not silently overwrite
	// prices you have confirmed.
	Reprice bool
}

// Execute regenerates a plan's items and pitfalls from the catalogue.
func (u *Build) Execute(ctx context.Context, in BuildInput) (domain.Plan, error) {
	plan, err := u.plans.GetPlan(ctx, in.PlanID)
	if err != nil {
		return domain.Plan{}, err
	}

	stay := StayFor(plan)
	catalog := u.catalog.Catalog()

	selected := service.SelectItems(catalog, stay)
	items := make([]domain.Item, 0, len(selected))
	catalogIDs := make([]string, 0, len(selected))
	for i, entry := range selected {
		items = append(items, itemFromCatalog(plan, entry, stay, i))
		catalogIDs = append(catalogIDs, entry.ID)
	}

	// Anything you have actually paid for here before beats every estimate.
	if u.prices != nil && len(catalogIDs) > 0 {
		observed, err := u.prices.Lookup(ctx, plan.Destination, catalogIDs)
		if err != nil {
			return domain.Plan{}, fmt.Errorf("price history: %w", err)
		}
		for i := range items {
			obs, ok := observed[items[i].CatalogID]
			if !ok {
				continue
			}
			cost := obs.UnitCost
			items[i].UnitCost = &cost
			items[i].Currency = obs.Currency
			items[i].Source = domain.CostFromHistory
			items[i].Confidence = nil
		}
	}

	if in.Reprice && u.pricer != nil {
		// A failed pricing pass must not lose the plan. The anchors are already
		// in place, so degrade to those and carry on.
		if err := u.pricer.Apply(ctx, plan, items); err != nil {
			return domain.Plan{}, fmt.Errorf("pricing: %w", err)
		}
	}

	if err := u.plans.ReplaceItems(ctx, plan.ID, items); err != nil {
		return domain.Plan{}, err
	}

	pitfalls := make([]domain.PlanPitfall, 0)
	for _, p := range service.SelectPitfalls(catalog, stay) {
		pitfalls = append(pitfalls, domain.PlanPitfall{
			PlanID:    plan.ID,
			PitfallID: p.ID,
			Title:     p.Title,
			Body:      p.Body,
			Severity:  p.Severity,
			Action:    p.Action,
		})
	}
	if err := u.plans.ReplacePitfalls(ctx, plan.ID, pitfalls); err != nil {
		return domain.Plan{}, err
	}

	return u.plans.GetPlan(ctx, plan.ID)
}

func itemFromCatalog(plan domain.Plan, entry domain.CatalogItem, stay service.Stay, order int) domain.Item {
	item := domain.Item{
		PlanID:    plan.ID,
		CatalogID: entry.ID,
		Category:  entry.Category,
		Name:      entry.Name,
		Quantity:  entry.Quantity.For(stay.Nights, stay.PartySize),
		Unit:      entry.Unit,
		// The anchor is a USD figure, so the line is USD until something prices
		// it locally. Labelling it with the plan currency would be a lie the
		// budget then reports as fact.
		Currency:          domain.AnchorCurrency,
		Source:            domain.CostFromCatalog,
		Status:            domain.ItemNeeded,
		Note:              entry.Note,
		CommonlyForgotten: entry.CommonlyForgotten,
		SortOrder:         order,
	}

	// A zero anchor means "no cost", as for a check or a document — distinct from
	// an unknown price, which stays nil.
	anchor := entry.AnchorUSD
	item.UnitCost = &anchor

	return item
}

// StayFor derives the rule inputs from a stored plan.
func StayFor(plan domain.Plan) service.Stay {
	nights := plan.Nights
	if nights <= 0 {
		nights = nightsBetween(plan.ArriveOn, plan.DepartOn)
	}
	return service.Stay{
		Nights:      nights,
		PartySize:   plan.PartySize,
		Housing:     plan.Housing,
		Climate:     plan.Climate,
		BudgetStyle: plan.BudgetStyle,
		CountryCode: plan.CountryCode,
	}
}

func nightsBetween(from, to time.Time) int {
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return 1
	}
	return int(to.Sub(from).Hours() / 24)
}
