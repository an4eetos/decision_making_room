package service_test

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/service"
)

var comfortRules = map[string]domain.ComfortRule{
	"towel":      {Dimension: domain.ComfortHygiene, Weight: domain.WeightCritical, Arrival: true},
	"toothbrush": {Dimension: domain.ComfortHygiene, Weight: domain.WeightCritical, Arrival: true},
	"clippers":   {Dimension: domain.ComfortHygiene, Weight: domain.WeightImportant},
	"bath_mat":   {Dimension: domain.ComfortHygiene, Weight: domain.WeightNice},
	"pillow":     {Dimension: domain.ComfortSleep, Weight: domain.WeightCritical},
	"sleep_kit":  {Dimension: domain.ComfortSleep, Weight: domain.WeightImportant},
	"kettle":     {Dimension: domain.ComfortFood, Weight: domain.WeightImportant},
}

// stock returns one line per rule, all covered unless named as needed.
func stock(statuses map[string]domain.ItemStatus) []domain.Item {
	var items []domain.Item
	for id := range comfortRules {
		status := domain.ItemHave
		if s, ok := statuses[id]; ok {
			status = s
		}
		items = append(items, domain.Item{CatalogID: id, Name: id, Status: status})
	}
	return items
}

func dimension(t *testing.T, r service.ComfortReport, dim domain.ComfortDimension) service.DimensionScore {
	t.Helper()
	for _, d := range r.Dimensions {
		if d.Dimension == dim {
			return d
		}
	}
	t.Fatalf("no %s dimension in report", dim)
	return service.DimensionScore{}
}

func TestComfortEverythingCoveredIsFull(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(nil), comfortRules)
	if r.Score == nil || *r.Score != 100 {
		t.Fatalf("score = %v, want 100", r.Score)
	}
	if len(r.Radar) != 0 {
		t.Fatalf("nothing to buy, radar = %v", r.Radar)
	}
}

// The point of the whole feature: one missing essential is not a 95% stay.
func TestComfortOneMissingCriticalCapsTheDimension(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{"towel": domain.ItemNeeded}), comfortRules)

	if got := dimension(t, r, domain.ComfortHygiene).Score; got > 35 {
		t.Fatalf("hygiene with no towel = %d, want <= 35", got)
	}
	if *r.Score >= 65 {
		t.Fatalf("overall with no towel = %d (%s), want below livable", *r.Score, r.Label)
	}
}

func TestComfortTwoMissingCriticalsCapLower(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{
		"towel": domain.ItemNeeded, "toothbrush": domain.ItemNeeded,
	}), comfortRules)

	if got := dimension(t, r, domain.ComfortHygiene).Score; got > 15 {
		t.Fatalf("hygiene with no towel or toothbrush = %d, want <= 15", got)
	}
}

func TestComfortMissingImportantCostsMoreThanItsShare(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{"clippers": domain.ItemNeeded}), comfortRules)

	// Coverage alone would be 13/16 ≈ 81; the penalty takes it lower.
	if got := dimension(t, r, domain.ComfortHygiene).Score; got >= 81 || got <= 35 {
		t.Fatalf("hygiene with no nail clippers = %d, want between the critical cap and plain coverage", got)
	}
}

// Skipping is a decision that the item does not apply, not a gap.
func TestComfortSkippedIsNotMissing(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{"towel": domain.ItemSkipped}), comfortRules)
	if *r.Score != 100 {
		t.Fatalf("skipped towel scored as missing: %d", *r.Score)
	}
}

func TestComfortBoughtCountsAsCovered(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{"pillow": domain.ItemBought}), comfortRules)
	if *r.Score != 100 {
		t.Fatalf("bought pillow scored as missing: %d", *r.Score)
	}
}

func TestComfortNoWeightedItemsHasNoReading(t *testing.T) {
	t.Parallel()

	items := []domain.Item{
		{CatalogID: "visa_check", Status: domain.ItemNeeded},
		{Name: "manual line", Status: domain.ItemNeeded},
	}
	r := service.Comfort(items, comfortRules)
	if r.Score != nil || r.Arrival != nil {
		t.Fatalf("expected no reading, got score=%v arrival=%v", r.Score, r.Arrival)
	}
}

func TestComfortArrivalReadsOnlyFirstNightItems(t *testing.T) {
	t.Parallel()

	// No pillow is bad for the stay, but the pillow is not an arrival item.
	r := service.Comfort(stock(map[string]domain.ItemStatus{"pillow": domain.ItemNeeded}), comfortRules)
	if r.Arrival == nil || *r.Arrival != 100 {
		t.Fatalf("arrival = %v, want 100", r.Arrival)
	}
	if *r.Score >= 65 {
		t.Fatalf("overall without a pillow = %d, want below livable", *r.Score)
	}

	r = service.Comfort(stock(map[string]domain.ItemStatus{"towel": domain.ItemNeeded}), comfortRules)
	if r.Arrival == nil || *r.Arrival > 35 {
		t.Fatalf("arrival with no towel = %v, want <= 35", r.Arrival)
	}
}

func TestComfortRadarBuysTheEssentialsFirst(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{
		"towel":    domain.ItemNeeded,
		"pillow":   domain.ItemNeeded,
		"bath_mat": domain.ItemNeeded,
		"kettle":   domain.ItemNeeded,
	}), comfortRules)

	if len(r.Radar) != 3 {
		t.Fatalf("radar has %d steps, want 3", len(r.Radar))
	}
	first := map[string]bool{r.Radar[0].Item.CatalogID: true, r.Radar[1].Item.CatalogID: true}
	if !first["towel"] || !first["pillow"] {
		t.Fatalf("radar should lead with towel and pillow, got %s, %s",
			r.Radar[0].Item.CatalogID, r.Radar[1].Item.CatalogID)
	}

	prev := *r.Score
	for _, step := range r.Radar {
		if step.ScoreAfter <= prev {
			t.Fatalf("radar step %s does not raise the score: %d -> %d", step.Item.CatalogID, prev, step.ScoreAfter)
		}
		prev = step.ScoreAfter
	}
}

func TestComfortMissingIsWorstFirst(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{
		"bath_mat": domain.ItemNeeded, "towel": domain.ItemNeeded, "clippers": domain.ItemNeeded,
	}), comfortRules)

	missing := dimension(t, r, domain.ComfortHygiene).Missing
	if len(missing) != 3 || missing[0].CatalogID != "towel" || missing[2].CatalogID != "bath_mat" {
		t.Fatalf("missing order wrong: %v", missing)
	}
}

// Only essentials make a stay miserable. A missing kettle must never rank below
// a missing towel, even when the kettle is the only thing in its dimension.
func TestComfortNonCriticalGapsHaveAFloor(t *testing.T) {
	t.Parallel()

	r := service.Comfort(stock(map[string]domain.ItemStatus{"kettle": domain.ItemNeeded}), comfortRules)
	if got := dimension(t, r, domain.ComfortFood).Score; got < 50 {
		t.Fatalf("food with only the kettle missing = %d, want >= 50", got)
	}
}

func TestComfortMissingOnlyNiceThingsStaysLivable(t *testing.T) {
	t.Parallel()

	rules := map[string]domain.ComfortRule{
		"towel":     {Dimension: domain.ComfortHygiene, Weight: domain.WeightCritical},
		"rain_gear": {Dimension: domain.ComfortClimate, Weight: domain.WeightNice},
	}
	items := []domain.Item{
		{CatalogID: "towel", Status: domain.ItemHave},
		{CatalogID: "rain_gear", Status: domain.ItemNeeded},
	}
	r := service.Comfort(items, rules)
	if *r.Score < 80 {
		t.Fatalf("missing only a rain jacket = %d (%s), want >= 80", *r.Score, r.Label)
	}
}
