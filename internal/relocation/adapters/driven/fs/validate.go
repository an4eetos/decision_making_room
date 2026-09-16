package fs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

var (
	validCategories = []domain.Category{
		domain.CategoryArrival, domain.CategoryHousing, domain.CategoryBathroom,
		domain.CategorySkincare, domain.CategoryKitchen, domain.CategoryBedding,
		domain.CategoryLaundry, domain.CategoryWorkspace, domain.CategoryHealth,
		domain.CategoryConnect, domain.CategoryAdmin, domain.CategoryClimateKit,
		domain.CategoryCleaning, domain.CategoryExit,
	}
	validHousing = []domain.Housing{
		domain.HousingHotel, domain.HousingServiced, domain.HousingFurnished,
		domain.HousingUnfurnished, domain.HousingShared,
	}
	validClimates = []domain.Climate{
		domain.ClimateTropical, domain.ClimateTemperate, domain.ClimateCold, domain.ClimateArid,
	}
	validBudgets = []domain.BudgetStyle{
		domain.BudgetFrugal, domain.BudgetStandard, domain.BudgetComfortable,
	}
	validSeverities = []domain.Severity{
		domain.SeverityCritical, domain.SeverityCostly, domain.SeverityAnnoying,
	}
)

func validate(c domain.Catalog) error {
	var problems []error

	seen := make(map[string]struct{}, len(c.Items))
	for _, item := range c.Items {
		where := "item " + item.ID
		if strings.TrimSpace(item.ID) == "" {
			problems = append(problems, errors.New("item with an empty id"))
			continue
		}
		if _, dup := seen[item.ID]; dup {
			problems = append(problems, fmt.Errorf("%s: duplicate id", where))
		}
		seen[item.ID] = struct{}{}

		if strings.TrimSpace(item.Name) == "" {
			problems = append(problems, fmt.Errorf("%s: missing name", where))
		}
		if !slices.Contains(validCategories, item.Category) {
			problems = append(problems, fmt.Errorf("%s: unknown category %q", where, item.Category))
		}
		// An item with no quantity rule would silently resolve to 1 of something.
		// Being explicit here is what keeps the consumable maths honest.
		if item.Quantity.Base <= 0 && item.Quantity.PerDays <= 0 {
			problems = append(problems, fmt.Errorf("%s: quantity needs a base or per_days", where))
		}
		if item.AnchorUSD < 0 {
			problems = append(problems, fmt.Errorf("%s: negative anchor_usd", where))
		}
		problems = append(problems, validateRules(where, item.When)...)
	}

	seenPitfalls := make(map[string]struct{}, len(c.Pitfalls))
	for _, p := range c.Pitfalls {
		where := "pitfall " + p.ID
		if strings.TrimSpace(p.ID) == "" {
			problems = append(problems, errors.New("pitfall with an empty id"))
			continue
		}
		if _, dup := seenPitfalls[p.ID]; dup {
			problems = append(problems, fmt.Errorf("%s: duplicate id", where))
		}
		seenPitfalls[p.ID] = struct{}{}

		if strings.TrimSpace(p.Title) == "" {
			problems = append(problems, fmt.Errorf("%s: missing title", where))
		}
		if strings.TrimSpace(p.Body) == "" {
			problems = append(problems, fmt.Errorf("%s: missing body", where))
		}
		// A warning without a concrete next step is just anxiety.
		if strings.TrimSpace(p.Action) == "" {
			problems = append(problems, fmt.Errorf("%s: missing action", where))
		}
		if !slices.Contains(validSeverities, p.Severity) {
			problems = append(problems, fmt.Errorf("%s: unknown severity %q", where, p.Severity))
		}
		problems = append(problems, validateRules(where, p.When)...)
	}

	return errors.Join(problems...)
}

func validateRules(where string, r domain.Rules) []error {
	var problems []error

	for _, h := range slices.Concat(r.Housing, r.NotHousing) {
		if !slices.Contains(validHousing, h) {
			problems = append(problems, fmt.Errorf("%s: unknown housing %q", where, h))
		}
	}
	for _, cl := range r.Climate {
		if !slices.Contains(validClimates, cl) {
			problems = append(problems, fmt.Errorf("%s: unknown climate %q", where, cl))
		}
	}
	for _, b := range r.Budget {
		if !slices.Contains(validBudgets, b) {
			problems = append(problems, fmt.Errorf("%s: unknown budget %q", where, b))
		}
	}
	// A window that can never match means the entry is dead and nobody would know.
	if r.MinNights > 0 && r.MaxNights > 0 && r.MinNights > r.MaxNights {
		problems = append(problems, fmt.Errorf("%s: min_nights %d exceeds max_nights %d", where, r.MinNights, r.MaxNights))
	}

	return problems
}
