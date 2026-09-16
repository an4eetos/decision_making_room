package domain

import "math"

// CatalogItem is a thing a stay might need, plus the rules for when it does.
// These ship as YAML so the list can be argued with and extended without a
// rebuild — the knowledge is the product here, not the code around it.
type CatalogItem struct {
	ID                string   `yaml:"id"`
	Category          Category `yaml:"category"`
	Name              string   `yaml:"name"`
	Unit              string   `yaml:"unit"`
	Note              string   `yaml:"note"`
	CommonlyForgotten bool     `yaml:"commonly_forgotten"`

	Quantity QuantityRule `yaml:"quantity"`
	When     Rules        `yaml:"when"`

	// AnchorUSD is a rough global baseline, not a real local price. It exists so
	// an unpriced plan still totals to something, and so the pricing pass has a
	// sane starting point to adjust rather than inventing from nothing.
	AnchorUSD float64 `yaml:"anchor_usd"`
}

// QuantityRule scales an item to the stay. The distinction that matters is
// durable goods versus consumables: a longer stay needs the same two towels but
// meaningfully more sunscreen.
type QuantityRule struct {
	// Base is a fixed count for durable goods.
	Base float64 `yaml:"base"`
	// PerDays means one unit covers this many days. 50ml of sunscreen applied
	// properly is about three weeks, which is why a three-month stay is a
	// different shopping list rather than simply a longer one.
	PerDays int `yaml:"per_days"`
	// PerPerson multiplies by party size. False for things a household shares.
	PerPerson bool    `yaml:"per_person"`
	Max       float64 `yaml:"max"`
}

// For computes how many units a given stay needs. It rounds consumables up:
// running out of contact lens solution in week eleven is not a rounding error.
func (q QuantityRule) For(nights, partySize int) float64 {
	if partySize < 1 {
		partySize = 1
	}

	qty := q.Base
	if q.PerDays > 0 {
		if nights < 1 {
			nights = 1
		}
		qty += math.Ceil(float64(nights) / float64(q.PerDays))
	}
	if qty <= 0 {
		qty = 1
	}
	if q.PerPerson {
		qty *= float64(partySize)
	}
	if q.Max > 0 && qty > q.Max {
		qty = q.Max
	}
	return qty
}

// Rules decide whether an item or pitfall applies. Every field is optional; an
// empty Rules matches everything.
type Rules struct {
	MinNights int `yaml:"min_nights"`
	MaxNights int `yaml:"max_nights"`

	Housing    []Housing `yaml:"housing"`
	NotHousing []Housing `yaml:"not_housing"`
	Climate    []Climate `yaml:"climate"`

	MinPartySize int           `yaml:"min_party_size"`
	Budget       []BudgetStyle `yaml:"budget"`

	Countries    []string `yaml:"countries"`
	NotCountries []string `yaml:"not_countries"`
}

// CatalogPitfall is a mistake with the conditions under which it bites. The
// rules matter as much as the text: a warning list you scroll past is worthless,
// so you should only ever see the ones that apply to this trip.
type CatalogPitfall struct {
	ID       string   `yaml:"id"`
	Title    string   `yaml:"title"`
	Body     string   `yaml:"body"`
	Severity Severity `yaml:"severity"`
	// Action is the concrete thing to do about it, not a restatement of the risk.
	Action string `yaml:"action"`
	When   Rules  `yaml:"when"`
}

// Catalog is the loaded, validated knowledge base.
type Catalog struct {
	Items    []CatalogItem
	Pitfalls []CatalogPitfall
}
