package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

var supplyNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func dayAgo(n int) string { return supplyNow.AddDate(0, 0, -n).Format("2006-01-02") }

func fptr(v float64) *float64 { return &v }

func supplyFor(l domain.Logistics, name string) domain.FrontSupply {
	for _, f := range l.Fronts {
		if f.Front == name {
			return f
		}
	}
	return domain.FrontSupply{}
}

func readingText(rs []domain.Reading, kind domain.ReadingKind) string {
	for _, r := range rs {
		if r.Kind == kind {
			return r.Text
		}
	}
	return ""
}

func TestSupplyWarnsBeforeAFrontCollapses(t *testing.T) {
	t.Parallel()
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	health := domain.Front{ID: uuid.New(), Name: "Health", Status: domain.FrontActive}
	people := domain.Front{ID: uuid.New(), Name: "People", Status: domain.FrontActive}

	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive}
	run := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &health.ID, Text: "Run a 10k", Status: domain.StatusActive}
	call := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &people.ID, Text: "Call home weekly", Status: domain.StatusActive}

	soon := supplyNow.Add(48 * time.Hour)
	later := supplyNow.AddDate(0, 0, 20)
	doneAt := supplyNow.AddDate(0, 0, -10)
	var orders []comdomain.Commitment
	add := func(target uuid.UUID, status comdomain.Status, due *time.Time) {
		c := comdomain.Commitment{ID: uuid.New(), Text: "order", Status: status, TargetID: &target, DueAt: due}
		if status == comdomain.StatusDone {
			c.ResolvedAt = &doneAt
		}
		orders = append(orders, c)
	}
	for range 3 {
		add(ship.ID, comdomain.StatusOpen, &soon)
	}
	add(ship.ID, comdomain.StatusOpen, &later)
	add(ship.ID, comdomain.StatusStale, nil)
	for range 3 {
		add(ship.ID, comdomain.StatusDone, nil)
	}
	add(ship.ID, comdomain.StatusProposed, &soon) // not agreed: not on the books
	add(run.ID, comdomain.StatusOpen, &soon)
	add(call.ID, comdomain.StatusOpen, &soon)
	add(call.ID, comdomain.StatusOpen, &soon)

	sorties := []domain.Sortie{
		// Work: 50 minutes in the window, four hours before it.
		{FrontID: work.ID, Minutes: 25, Day: dayAgo(1)},
		{FrontID: work.ID, Minutes: 25, Day: dayAgo(3)},
		{FrontID: work.ID, Minutes: 240, Day: dayAgo(20)},
		// Health had an hour three weeks ago and nothing since.
		{FrontID: health.ID, Minutes: 60, Day: dayAgo(20)},
		// Outside the record: read by nothing.
		{FrontID: work.ID, Minutes: 400, Day: dayAgo(40)},
	}

	in := SupplyInput{
		Fronts: []domain.Front{work, health, people}, Items: []domain.Item{ship, run, call}, Orders: orders,
		Sorties: sorties, FirstSortie: dayAgo(40), SortieMinutes: 25, Now: supplyNow,
	}
	l := Supply(in)
	if !l.Sorties || l.WindowDays != 14 || l.StaleDays != 14 || len(l.Fronts) != 3 {
		t.Fatalf("logistics shape: %+v", l)
	}

	w := supplyFor(l, "Work")
	if w.Sorties != 2 || w.HoursPerWeek != 0.42 || w.EarlierHours != 4 {
		t.Fatalf("Work time: %+v", w)
	}
	// Its own rate: 290 minutes over three orders carried out.
	if !w.PerOrderMeasured || w.HoursPerOrder != 1.61 {
		t.Fatalf("Work rate: %+v", w)
	}
	if w.OrdersOpen != 5 || w.OrdersDueSoon != 3 || !w.Outrun || w.Cut {
		t.Fatalf("Work orders: %+v", w)
	}

	h := supplyFor(l, "Health")
	if h.Sorties != 0 || h.EarlierHours != 1 || !h.Cut || h.Outrun || h.PerOrderMeasured {
		t.Fatalf("Health lost its supply: %+v", h)
	}
	// A front never supplied is not called out of supply.
	if p := supplyFor(l, "People"); p.Cut || p.Outrun || p.OrdersDueSoon != 2 {
		t.Fatalf("People: %+v", p)
	}

	s := Supplied(Read(in.Fronts, in.Items, orders, nil, supplyNow), l)
	if s.Logistics == nil {
		t.Fatal("the sitrep carries its logistics")
	}
	outrun := readingText(s.Readings, domain.ReadingOutrun)
	for _, want := range []string{"Work is outrunning its supply", "3 orders due within a week need about 4.8 h", "1.6 h an order (its own rate)", "25 min a week"} {
		if !strings.Contains(outrun, want) {
			t.Fatalf("outrun reading lacks %q: %q", want, outrun)
		}
	}
	// It takes the place of overextended on the same front, not a second
	// reading saying the same thing.
	if hasReading(s.Readings, domain.ReadingOverextended) {
		t.Fatalf("outrun replaces overextended: %+v", s.Readings)
	}
	if cut := readingText(s.Readings, domain.ReadingCut); !strings.Contains(cut, "Health has lost its supply") || !strings.Contains(cut, "1 order falls due") {
		t.Fatalf("cut reading: %q", cut)
	}

	brief := Brief(s)
	for _, want := range []string{"Supply, as the person reports it", "- Work: 25 min a week over the last 14 days (2 sorties); 5 orders open ≈ 8.1 h at 1.6 h an order (its own rate), 3 due within a week", "- Health: no sorties in the last 14 days", "- People: no sorties in the last 14 days; 2 orders open"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Index(brief, "Staff readings") > strings.Index(brief, "Supply, as the person reports it") {
		t.Fatal("the supply lines come after the readings, so a long map is cut there first")
	}
}

func TestSupplyEstimatesAnOrderUntilAFrontHasARate(t *testing.T) {
	t.Parallel()
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive}
	var orders []comdomain.Commitment
	for range 10 {
		orders = append(orders, comdomain.Commitment{ID: uuid.New(), Status: comdomain.StatusOpen, TargetID: &ship.ID})
	}
	l := Supply(SupplyInput{
		Fronts: []domain.Front{work}, Items: []domain.Item{ship}, Orders: orders,
		Sorties: []domain.Sortie{{FrontID: work.ID, Minutes: 50, Day: dayAgo(0)}}, FirstSortie: dayAgo(30),
		SortieMinutes: 30, StaleAfter: 21 * 24 * time.Hour, Now: supplyNow,
	})
	w := l.Fronts[0]
	// Nothing done yet: one sortie an order, an estimate.
	if w.PerOrderMeasured || w.HoursPerOrder != 0.5 || w.BacklogHours != 5 || w.HoursPerWeek != 0.42 {
		t.Fatalf("estimate: %+v", w)
	}
	// No due dates, but twelve weeks of backlog against three weeks before
	// an order goes stale.
	if !w.Outrun || w.BacklogWeeks != 12 || l.StaleDays != 21 {
		t.Fatalf("backlog outrun: %+v", w)
	}
	text := outrunText(w, l)
	for _, want := range []string{"10 open orders are about 5 h", "(est: one sortie an order)", "12 weeks", "goes stale after 21 days"} {
		if !strings.Contains(text, want) {
			t.Fatalf("backlog reading lacks %q: %q", want, text)
		}
	}

	// One order more than a thin week supplies is a stretch, not a front
	// collapsing: the slack of one extra block.
	one := Supply(SupplyInput{
		Fronts: []domain.Front{work}, Items: []domain.Item{ship},
		Orders:  []comdomain.Commitment{{ID: uuid.New(), Status: comdomain.StatusOpen, TargetID: &ship.ID, DueAt: &supplyNow}},
		Sorties: []domain.Sortie{{FrontID: work.ID, Minutes: 25, Day: dayAgo(0)}}, FirstSortie: dayAgo(30), Now: supplyNow,
	})
	if one.Fronts[0].Outrun || one.Fronts[0].OrdersDueSoon != 1 {
		t.Fatalf("one order against one sortie a fortnight: %+v", one.Fronts[0])
	}

	// A light backlog well inside the horizon is not a warning.
	light := Supply(SupplyInput{
		Fronts: []domain.Front{work}, Items: []domain.Item{ship}, Orders: orders[:2],
		Sorties: []domain.Sortie{{FrontID: work.ID, Minutes: 120, Day: dayAgo(2)}}, FirstSortie: dayAgo(30),
		Now: supplyNow,
	})
	if light.Fronts[0].Outrun || light.SortieMinutes != 25 {
		t.Fatalf("two orders against an hour a week is supplied: %+v", light.Fronts[0])
	}
}

func TestSupplyDecidesOverextensionWhereItIsMeasured(t *testing.T) {
	t.Parallel()
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	people := domain.Front{ID: uuid.New(), Name: "People", Status: domain.FrontActive}
	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive}
	call := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &people.ID, Text: "Call home", Status: domain.StatusActive}
	var orders []comdomain.Commitment
	for range 4 {
		orders = append(orders,
			comdomain.Commitment{ID: uuid.New(), Status: comdomain.StatusOpen, TargetID: &ship.ID},
			comdomain.Commitment{ID: uuid.New(), Status: comdomain.StatusOpen, TargetID: &call.ID})
	}
	fronts, items := []domain.Front{work, people}, []domain.Item{ship, call}
	// Four hours a week on Work clears four orders well inside the horizon.
	l := Supply(SupplyInput{
		Fronts: fronts, Items: items, Orders: orders, FirstSortie: dayAgo(30), Now: supplyNow,
		Sorties: []domain.Sortie{{FrontID: work.ID, Minutes: 240, Day: dayAgo(1)}, {FrontID: work.ID, Minutes: 240, Day: dayAgo(8)}},
	})
	s := Supplied(Read(fronts, items, orders, nil, supplyNow), l)
	var over []string
	for _, r := range s.Readings {
		if r.Kind == domain.ReadingOverextended {
			over = append(over, r.Text)
		}
		if r.Kind == domain.ReadingOutrun {
			t.Fatalf("Work's sorties keep up: %q", r.Text)
		}
	}
	// People, never supplied by a sortie, is still read by count.
	if len(over) != 1 || !strings.HasPrefix(over[0], "People is overextended") {
		t.Fatalf("overextended by count only where nothing measures supply: %q", over)
	}
}

func TestSupplyWindowFollowsAYoungRecord(t *testing.T) {
	t.Parallel()
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	sorties := []domain.Sortie{{FrontID: work.ID, Minutes: 60, Day: dayAgo(1)}}
	// Two days of record: measured over a week, not diluted over two.
	l := Supply(SupplyInput{Fronts: []domain.Front{work}, Sorties: sorties, FirstSortie: dayAgo(1), Now: supplyNow})
	if l.WindowDays != 7 || l.Fronts[0].HoursPerWeek != 1 {
		t.Fatalf("young record: window %d, %+v", l.WindowDays, l.Fronts[0])
	}
	l = Supply(SupplyInput{Fronts: []domain.Front{work}, Sorties: sorties, FirstSortie: dayAgo(9), Now: supplyNow})
	if l.WindowDays != 10 || l.Fronts[0].HoursPerWeek != 0.7 {
		t.Fatalf("ten days of record: window %d, %+v", l.WindowDays, l.Fronts[0])
	}
	// A withdrawn front has no supply line.
	gone := domain.Front{ID: uuid.New(), Name: "Old", Status: domain.FrontWithdrawn}
	if l := Supply(SupplyInput{Fronts: []domain.Front{gone}, Now: supplyNow}); len(l.Fronts) != 0 || l.InUse() {
		t.Fatalf("withdrawn front: %+v", l)
	}
}

func TestSupplyMoneyNeverConvertsAndKeepsEstimates(t *testing.T) {
	t.Parallel()
	moving := domain.Front{ID: uuid.New(), Name: "Moving", Status: domain.FrontActive, Funds: fptr(2500), FundsCurrency: "EUR"}
	visa := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &moving.ID, Text: "Get the visa", Status: domain.StatusActive, Cost: fptr(300), CostCurrency: "EUR"}
	taken := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &moving.ID, Text: "Find a flat", Status: domain.StatusResolved, Cost: fptr(1000), CostCurrency: "EUR"}

	lands := 9
	deployments := []domain.Deployment{
		{PlanID: uuid.New(), FrontID: moving.ID, Destination: "Lisbon", ArriveOn: supplyNow.AddDate(0, 0, lands).Format("2006-01-02"),
			Currency: "EUR", Cost: 2400, EstimatedShare: 0.5, ItemsNeeded: 14, CriticalOpen: 2},
		{PlanID: uuid.New(), FrontID: moving.ID, Destination: "Porto", ArriveOn: supplyNow.AddDate(0, 0, 40).Format("2006-01-02"),
			Currency: "USD", Cost: 100},
		// Over: departed last week.
		{PlanID: uuid.New(), FrontID: moving.ID, Destination: "Tbilisi", ArriveOn: dayAgo(60), DepartOn: dayAgo(7), Currency: "EUR", Cost: 5000},
	}
	in := SupplyInput{Fronts: []domain.Front{moving}, Items: []domain.Item{visa, taken}, Deployments: deployments, Now: supplyNow}
	l := Supply(in)
	m := l.Fronts[0]
	if !l.InUse() || l.Sorties {
		t.Fatalf("money alone puts logistics in use, without time: %+v", l)
	}
	if m.Currency != "EUR" || m.Costs != 2700 || m.CostsEstimated != 1200 || !m.Overdrawn {
		t.Fatalf("money: %+v", m)
	}
	if len(m.Unconverted) != 1 || m.Unconverted[0] != (domain.Money{Amount: 100, Currency: "USD"}) {
		t.Fatalf("a cost in another currency is named, not summed: %+v", m.Unconverted)
	}
	if len(m.Deployments) != 2 || m.Deployments[0].LandsIn == nil || *m.Deployments[0].LandsIn != lands {
		t.Fatalf("deployments: %+v", m.Deployments)
	}

	s := Supplied(Read(in.Fronts, in.Items, nil, nil, supplyNow), l)
	if got := readingText(s.Readings, domain.ReadingOverdrawn); got != "Moving is overdrawn: its costs come to 2,700 EUR against 2,500 EUR of funds, 44% of the costs an estimate." {
		t.Fatalf("overdrawn: %q", got)
	}
	if got := readingText(s.Readings, domain.ReadingDeployment); got != "The deployment to Lisbon (Moving) lands in 9 days: 2 critical pitfalls not handled, 14 items still needed, 50% of its cost an estimate." {
		t.Fatalf("deployment: %q", got)
	}
	// A landing a month out is not read yet; time readings need sorties.
	if strings.Contains(readingText(s.Readings, domain.ReadingDeployment), "Porto") || hasReading(s.Readings, domain.ReadingOutrun) {
		t.Fatalf("readings: %+v", s.Readings)
	}
	brief := Brief(s)
	for _, want := range []string{"- Moving: funds 2,500 EUR, costs ahead 2,700 EUR (44% est), plus 100 USD not converted", "deployment to Lisbon, lands in 9 days", "setup 2,400 EUR (50% est)"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "a week over the last") || strings.Contains(brief, "no sorties in") {
		t.Fatalf("no sorties on record, so the brief says nothing about hours:\n%s", brief)
	}

	// Without funds, the costs are read in the currency carrying most of them.
	moving.Funds, moving.FundsCurrency = nil, ""
	l = Supply(SupplyInput{Fronts: []domain.Front{moving}, Items: []domain.Item{visa}, Deployments: deployments[1:2], Now: supplyNow})
	if m := l.Fronts[0]; m.Currency != "EUR" || m.Costs != 300 || m.Overdrawn || m.Unconverted[0].Currency != "USD" {
		t.Fatalf("no funds: %+v", m)
	}
}

func TestEnergyReadsLowFromMostRecentEvenings(t *testing.T) {
	t.Parallel()
	read := func(levels map[int]int) domain.EnergySupply {
		var reports []domain.EnergyReport
		for ago, level := range levels {
			reports = append(reports, domain.EnergyReport{Day: dayAgo(ago), Level: level})
		}
		return readEnergy(reports, midnight(supplyNow), time.UTC)
	}
	e := read(map[int]int{3: 4, 2: 2, 1: 1, 9: 1})
	if !e.Low || len(e.Reports) != 3 || e.Today != 0 || e.Reports[0].Day != dayAgo(3) {
		t.Fatalf("two low of the last three: %+v", e)
	}
	if text := energyText(e); !strings.Contains(text, "2 of the last 3 evenings") || !strings.Contains(text, "low, ") {
		t.Fatalf("energy text: %q", text)
	}
	if text := energyText(read(map[int]int{1: 1, 0: 2})); !strings.Contains(text, "both of the last two evenings") {
		t.Fatalf("two of two: %q", text)
	}
	if e := read(map[int]int{1: 2, 0: 4}); e.Low || e.Today != 4 {
		t.Fatalf("one low of two is not low: %+v", e)
	}
	if e := read(map[int]int{0: 1}); e.Low {
		t.Fatal("one evening is not a pattern")
	}

	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	l := Supply(SupplyInput{Fronts: []domain.Front{work}, Energy: []domain.EnergyReport{{Day: dayAgo(1), Level: 1}, {Day: dayAgo(0), Level: 2}}, Now: supplyNow})
	s := Supplied(Read([]domain.Front{work}, nil, nil, nil, supplyNow), l)
	if !hasReading(s.Readings, domain.ReadingEnergy) || !strings.Contains(Brief(s), "- Energy, last 7 days:") {
		t.Fatalf("low energy is read and briefed: %+v", s.Readings)
	}
}

func TestSupplyAddsNothingUntilSomethingIsReported(t *testing.T) {
	t.Parallel()
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive}
	var orders []comdomain.Commitment
	for range 5 {
		orders = append(orders, comdomain.Commitment{ID: uuid.New(), Status: comdomain.StatusOpen, TargetID: &ship.ID})
	}
	plain := Read([]domain.Front{work}, []domain.Item{ship}, orders, nil, supplyNow)
	l := Supply(SupplyInput{Fronts: []domain.Front{work}, Items: []domain.Item{ship}, Orders: orders, Now: supplyNow})
	supplied := Supplied(plain, l)
	if l.InUse() || Brief(supplied) != Brief(plain) {
		t.Fatalf("nothing reported, nothing added:\n%s", Brief(supplied))
	}
	// The overextended reading stands when there is no supply to read.
	if !hasReading(supplied.Readings, domain.ReadingOverextended) {
		t.Fatalf("overextended stays: %+v", supplied.Readings)
	}
}

func TestSupplyFormats(t *testing.T) {
	t.Parallel()
	for in, want := range map[float64]string{0: "0 h", 0.4167: "25 min", 1: "1 h", 1.61: "1.6 h", 12.04: "12 h"} {
		if got := hoursText(in); got != want {
			t.Errorf("hoursText(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0: "0 EUR", 999.6: "1,000 EUR", 2500: "2,500 EUR", 1234567: "1,234,567 EUR"} {
		if got := moneyText(in, "EUR"); got != want {
			t.Errorf("moneyText(%v) = %q, want %q", in, got, want)
		}
	}
}
