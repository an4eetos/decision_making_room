// Package service holds the pure logic: which catalogue entries apply to a stay,
// and how many of each it needs. No database, no model calls — this is the part
// that has to be right regardless of what the LLM says.
package service

import (
	"slices"
	"strings"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

// Stay is everything the rules need to know about a trip.
type Stay struct {
	Nights      int
	PartySize   int
	Housing     domain.Housing
	Climate     domain.Climate
	BudgetStyle domain.BudgetStyle
	CountryCode string
}

// Applies reports whether a rule set matches this stay. Unset fields never
// exclude anything, so a rule only narrows what it explicitly mentions.
func Applies(r domain.Rules, s Stay) bool {
	if r.MinNights > 0 && s.Nights < r.MinNights {
		return false
	}
	if r.MaxNights > 0 && s.Nights > r.MaxNights {
		return false
	}
	if r.MinPartySize > 0 && s.PartySize < r.MinPartySize {
		return false
	}

	// An unknown housing type or climate must not silently drop every item that
	// mentions one; the list degrades to "everything plausible" instead, which is
	// the safe direction for a checklist.
	if len(r.Housing) > 0 && s.Housing != "" && !slices.Contains(r.Housing, s.Housing) {
		return false
	}
	if len(r.NotHousing) > 0 && slices.Contains(r.NotHousing, s.Housing) {
		return false
	}
	if len(r.Climate) > 0 && s.Climate != "" && !slices.Contains(r.Climate, s.Climate) {
		return false
	}
	if len(r.Budget) > 0 && s.BudgetStyle != "" && !slices.Contains(r.Budget, s.BudgetStyle) {
		return false
	}

	country := strings.ToUpper(strings.TrimSpace(s.CountryCode))
	if len(r.Countries) > 0 {
		if country == "" || !containsFold(r.Countries, country) {
			return false
		}
	}
	if len(r.NotCountries) > 0 && country != "" && containsFold(r.NotCountries, country) {
		return false
	}

	return true
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(strings.TrimSpace(v), want) {
			return true
		}
	}
	return false
}

// SelectItems returns the catalogue entries that apply, in catalogue order so
// the resulting checklist is grouped and stable between runs.
func SelectItems(catalog domain.Catalog, s Stay) []domain.CatalogItem {
	var out []domain.CatalogItem
	for _, item := range catalog.Items {
		if Applies(item.When, s) {
			out = append(out, item)
		}
	}
	return out
}

// SelectPitfalls returns applicable pitfalls, most severe first.
func SelectPitfalls(catalog domain.Catalog, s Stay) []domain.CatalogPitfall {
	var out []domain.CatalogPitfall
	for _, p := range catalog.Pitfalls {
		if Applies(p.When, s) {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b domain.CatalogPitfall) int {
		return severityRank(a.Severity) - severityRank(b.Severity)
	})
	return out
}

func severityRank(s domain.Severity) int {
	switch s {
	case domain.SeverityCritical:
		return 0
	case domain.SeverityCostly:
		return 1
	default:
		return 2
	}
}
