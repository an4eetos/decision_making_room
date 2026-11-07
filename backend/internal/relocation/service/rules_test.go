package service_test

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/service"
)

func TestQuantityDurableGoodsDoNotScaleWithStay(t *testing.T) {
	t.Parallel()

	towels := domain.QuantityRule{Base: 2, PerPerson: true}

	if got := towels.For(14, 1); got != 2 {
		t.Fatalf("two weeks, one person = %v towels, want 2", got)
	}
	if got := towels.For(180, 1); got != 2 {
		t.Fatalf("six months still needs the same two towels, got %v", got)
	}
	if got := towels.For(90, 2); got != 4 {
		t.Fatalf("two people = %v towels, want 4", got)
	}
}

// The distinction the whole catalogue turns on: a longer stay needs meaningfully
// more of a consumable, and rounding down means running out.
func TestQuantityConsumablesScaleAndRoundUp(t *testing.T) {
	t.Parallel()

	sunscreen := domain.QuantityRule{PerDays: 21, PerPerson: true}

	if got := sunscreen.For(21, 1); got != 1 {
		t.Fatalf("exactly one period = %v bottles, want 1", got)
	}
	if got := sunscreen.For(22, 1); got != 2 {
		t.Fatalf("one day over = %v bottles, want 2 (must round up)", got)
	}
	if got := sunscreen.For(90, 1); got != 5 {
		t.Fatalf("three months = %v bottles, want 5", got)
	}
	if got := sunscreen.For(90, 2); got != 10 {
		t.Fatalf("three months for two = %v bottles, want 10", got)
	}
}

func TestQuantityRespectsMaxAndMinimums(t *testing.T) {
	t.Parallel()

	capped := domain.QuantityRule{PerDays: 10, Max: 3}
	if got := capped.For(365, 1); got != 3 {
		t.Fatalf("capped quantity = %v, want 3", got)
	}

	// A zero party size is a caller bug, not a reason to need nothing.
	if got := (domain.QuantityRule{Base: 1, PerPerson: true}).For(30, 0); got != 1 {
		t.Fatalf("zero party size = %v, want 1", got)
	}
}

func TestRulesEmptyMatchesEverything(t *testing.T) {
	t.Parallel()

	if !service.Applies(domain.Rules{}, service.Stay{Nights: 1}) {
		t.Fatal("an empty rule set must match everything")
	}
}

func TestRulesNightWindows(t *testing.T) {
	t.Parallel()

	long := domain.Rules{MinNights: 30}
	if service.Applies(long, service.Stay{Nights: 10}) {
		t.Fatal("min_nights should exclude a short stay")
	}
	if !service.Applies(long, service.Stay{Nights: 30}) {
		t.Fatal("min_nights should be inclusive")
	}
}

func TestRulesHousingAllowAndDeny(t *testing.T) {
	t.Parallel()

	notHotel := domain.Rules{NotHousing: []domain.Housing{domain.HousingHotel}}
	if service.Applies(notHotel, service.Stay{Nights: 30, Housing: domain.HousingHotel}) {
		t.Fatal("not_housing should exclude the listed type")
	}
	if !service.Applies(notHotel, service.Stay{Nights: 30, Housing: domain.HousingFurnished}) {
		t.Fatal("not_housing should allow everything else")
	}
}

// An unknown housing type or climate must widen the list, not empty it. A
// checklist that silently omits things because you did not fill in a dropdown is
// the exact failure this feature exists to prevent.
func TestRulesUnknownStayFieldsDoNotExcludeItems(t *testing.T) {
	t.Parallel()

	tropical := domain.Rules{Climate: []domain.Climate{domain.ClimateTropical}}
	if !service.Applies(tropical, service.Stay{Nights: 30}) {
		t.Fatal("an unspecified climate must not exclude climate-specific items")
	}

	furnished := domain.Rules{Housing: []domain.Housing{domain.HousingFurnished}}
	if !service.Applies(furnished, service.Stay{Nights: 30}) {
		t.Fatal("an unspecified housing type must not exclude housing-specific items")
	}
}

func TestRulesCountryMatching(t *testing.T) {
	t.Parallel()

	onlyDE := domain.Rules{Countries: []string{"DE"}}
	if !service.Applies(onlyDE, service.Stay{Nights: 30, CountryCode: "de"}) {
		t.Fatal("country matching should be case-insensitive")
	}
	if service.Applies(onlyDE, service.Stay{Nights: 30, CountryCode: "GE"}) {
		t.Fatal("a different country should not match")
	}
	// An allow-list with no country known cannot be satisfied, unlike the
	// widening behaviour above: naming specific countries is a positive claim.
	if service.Applies(onlyDE, service.Stay{Nights: 30}) {
		t.Fatal("an unknown country should not satisfy a country allow-list")
	}
}

func TestSelectPitfallsOrdersBySeverity(t *testing.T) {
	t.Parallel()

	catalog := domain.Catalog{Pitfalls: []domain.CatalogPitfall{
		{ID: "c", Severity: domain.SeverityAnnoying},
		{ID: "a", Severity: domain.SeverityCritical},
		{ID: "b", Severity: domain.SeverityCostly},
	}}

	got := service.SelectPitfalls(catalog, service.Stay{Nights: 30})
	want := []string{"a", "b", "c"}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("pitfall %d = %q, want %q", i, got[i].ID, id)
		}
	}
}
