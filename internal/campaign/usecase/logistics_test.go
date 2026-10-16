package usecase

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
)

// memLogistics is the logistics repository in memory.
type memLogistics struct {
	mu      sync.Mutex
	sorties []domain.Sortie
	energy  map[string]domain.EnergyReport
}

func newMemLogistics() *memLogistics { return &memLogistics{energy: map[string]domain.EnergyReport{}} }

func (m *memLogistics) CreateSortie(_ context.Context, s domain.Sortie) (domain.Sortie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID, s.CreatedAt = uuid.New(), time.Now()
	m.sorties = append(m.sorties, s)
	return s, nil
}

func (m *memLogistics) DeleteSortie(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.sorties {
		if s.ID == id {
			m.sorties = append(m.sorties[:i], m.sorties[i+1:]...)
			return nil
		}
	}
	return port.ErrNotFound
}

func (m *memLogistics) ListSorties(_ context.Context, since string) ([]domain.Sortie, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.Sortie{}
	for _, s := range m.sorties {
		if s.Day >= since {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Day > out[j].Day })
	return out, nil
}

func (m *memLogistics) FirstSortieDay(context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	first := ""
	for _, s := range m.sorties {
		if first == "" || s.Day < first {
			first = s.Day
		}
	}
	return first, nil
}

func (m *memLogistics) SaveEnergy(_ context.Context, r domain.EnergyReport) (domain.EnergyReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.ReportedAt = time.Now()
	m.energy[r.Day] = r
	return r, nil
}

func (m *memLogistics) DeleteEnergy(_ context.Context, day string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.energy[day]; !ok {
		return port.ErrNotFound
	}
	delete(m.energy, day)
	return nil
}

func (m *memLogistics) ListEnergy(_ context.Context, since string) ([]domain.EnergyReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []domain.EnergyReport{}
	for day, r := range m.energy {
		if day >= since {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out, nil
}

// fakeDeployments returns canned stays, or an error.
type fakeDeployments struct {
	list []domain.Deployment
	err  error
}

func (f fakeDeployments) Deployments(context.Context) ([]domain.Deployment, error) {
	return f.list, f.err
}

func newLogisticsFixture(t *testing.T, clock *time.Time, deployments port.DeploymentSource) (*Manage, *memOrders, *Logistics) {
	t.Helper()
	repo, orders := newMemRepo(), newMemOrders()
	manage := NewManage(repo, comusecase.NewManage(orders))
	manage.now = func() time.Time { return *clock }
	l := NewLogistics(newMemLogistics(), manage, deployments, time.UTC, 25, 14*24*time.Hour)
	l.now = manage.now
	return manage, orders, l
}

func TestLogSortieKeepsTheRecordHonest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	manage, orders, l := newLogisticsFixture(t, &clock, nil)

	work, _ := manage.CreateFront(ctx, "Work")
	s, err := l.LogSortie(ctx, SortieInput{FrontID: work.ID})
	if err != nil {
		t.Fatal(err)
	}
	if s.Minutes != 25 || s.Day != "2026-10-17" {
		t.Fatalf("a bare sortie is one default block, today: %+v", s)
	}
	if _, err := l.LogSortie(ctx, SortieInput{FrontID: work.ID, Minutes: 50, Day: "2026-10-03", Note: "  forgot this one  "}); err != nil {
		t.Fatalf("a forgotten block within the record can be logged: %v", err)
	}

	for name, in := range map[string]SortieInput{
		"ahead":       {FrontID: work.ID, Day: "2026-10-18"},
		"too old":     {FrontID: work.ID, Day: "2026-09-01"},
		"bad day":     {FrontID: work.ID, Day: "Friday"},
		"too short":   {FrontID: work.ID, Minutes: 2},
		"a whole day": {FrontID: work.ID, Minutes: 600},
		"long note":   {FrontID: work.ID, Note: strings.Repeat("x", 201)},
	} {
		if _, err := l.LogSortie(ctx, in); err == nil {
			t.Errorf("%s: a sortie like that is refused", name)
		}
	}
	if _, err := l.LogSortie(ctx, SortieInput{FrontID: uuid.New()}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("an unknown front: %v", err)
	}
	withdrawn := domain.FrontWithdrawn
	gone, _ := manage.CreateFront(ctx, "Old")
	if _, err := manage.UpdateFront(ctx, gone.ID, nil, &withdrawn); err != nil {
		t.Fatal(err)
	}
	if _, err := l.LogSortie(ctx, SortieInput{FrontID: gone.ID}); err == nil {
		t.Fatal("no sortie on a withdrawn front")
	}

	proposed, _ := orders.Create(ctx, comdomain.Commitment{Text: "Maybe", Status: comdomain.StatusProposed, Fingerprint: "maybe"})
	if _, err := l.LogSortie(ctx, SortieInput{FrontID: work.ID, OrderID: &proposed.ID}); err == nil {
		t.Fatal("a sortie is flown on an order you kept, not a proposal")
	}
	open, _ := orders.Create(ctx, comdomain.Commitment{Text: "Write the tests", Status: comdomain.StatusOpen, Fingerprint: "tests"})
	on, err := l.LogSortie(ctx, SortieInput{FrontID: work.ID, OrderID: &open.ID})
	if err != nil || on.OrderID == nil || *on.OrderID != open.ID {
		t.Fatalf("a sortie on an open order: %+v %v", on, err)
	}

	list, _ := l.Sorties(ctx, 14)
	if len(list) != 2 {
		t.Fatalf("the last two weeks hold two of the three: %+v", list)
	}
	if err := l.DeleteSortie(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.DeleteSortie(ctx, s.ID); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("deleted twice: %v", err)
	}
}

func TestEnergyIsOneTapADay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 17, 0, 30, 0, 0, time.UTC)
	_, _, l := newLogisticsFixture(t, &clock, nil)

	// Tapped after midnight on yesterday's evening check-in: it lands on
	// the evening it was about.
	if r, err := l.ReportEnergy(ctx, "2026-10-16", 2); err != nil || r.Day != "2026-10-16" {
		t.Fatalf("yesterday's evening: %+v %v", r, err)
	}
	if _, err := l.ReportEnergy(ctx, "", 4); err != nil {
		t.Fatal(err)
	}
	if _, err := l.ReportEnergy(ctx, "", 3); err != nil {
		t.Fatal(err)
	}
	reports, _ := l.Energy(ctx, 7)
	if len(reports) != 2 || reports[1].Day != "2026-10-17" || reports[1].Level != 3 {
		t.Fatalf("a second tap the same day replaces the first: %+v", reports)
	}

	for name, level := range map[string]int{"zero": 0, "six": 6} {
		if _, err := l.ReportEnergy(ctx, "", level); err == nil {
			t.Errorf("%s is off the scale", name)
		}
	}
	if _, err := l.ReportEnergy(ctx, "2026-10-18", 3); err == nil {
		t.Fatal("energy is reported for a day that has happened")
	}
	if _, err := l.ReportEnergy(ctx, "2026-10-01", 3); err == nil {
		t.Fatal("a missed evening is reported within the week")
	}
	if err := l.ClearEnergy(ctx, "2026-10-17"); err != nil {
		t.Fatal(err)
	}
	if err := l.ClearEnergy(ctx, "2026-10-17"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("cleared twice: %v", err)
	}
}

func TestFundsAndCostsAreYours(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	manage, _, _ := newLogisticsFixture(t, &clock, nil)

	moving, _ := manage.CreateFront(ctx, "Moving")
	f, err := manage.SetFunds(ctx, moving.ID, fptr(2500), " eur ")
	if err != nil || f.Funds == nil || *f.Funds != 2500 || f.FundsCurrency != "EUR" {
		t.Fatalf("funds: %+v %v", f, err)
	}
	for _, bad := range []struct {
		amount   float64
		currency string
	}{{-1, "EUR"}, {10, "EURO"}, {10, "E1R"}, {10, ""}} {
		if _, err := manage.SetFunds(ctx, moving.ID, &bad.amount, bad.currency); err == nil {
			t.Errorf("funds %v %q are refused", bad.amount, bad.currency)
		}
	}

	visa, _ := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Get the visa", FrontID: &moving.ID})
	visa, err = manage.Update(ctx, visa.ID, Patch{Cost: fptr(300)})
	if err != nil || visa.Cost == nil || *visa.Cost != 300 || visa.CostCurrency != "EUR" {
		t.Fatalf("a cost with no currency takes the front's: %+v %v", visa, err)
	}
	visa, _ = manage.Update(ctx, visa.ID, Patch{Cost: fptr(80), CostCurrency: "usd"})
	if visa.CostCurrency != "USD" {
		t.Fatalf("a named currency stands: %+v", visa)
	}
	visa, _ = manage.Update(ctx, visa.ID, Patch{ClearCost: true})
	if visa.Cost != nil || visa.CostCurrency != "" {
		t.Fatalf("cleared: %+v", visa)
	}
	fog, _ := manage.CreateItem(ctx, CreateInput{Type: domain.TypeUnknown, Text: "Which visa?", FrontID: &moving.ID})
	if _, err := manage.Update(ctx, fog.ID, Patch{Cost: fptr(10)}); err == nil {
		t.Fatal("only an objective has a cost")
	}

	if f, err := manage.SetFunds(ctx, moving.ID, nil, ""); err != nil || f.Funds != nil || f.FundsCurrency != "" {
		t.Fatalf("cleared funds: %+v %v", f, err)
	}
	// With no funds to borrow a currency from, a cost is in dollars.
	loose, _ := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "A course"})
	if loose, _ = manage.Update(ctx, loose.ID, Patch{Cost: fptr(200)}); loose.CostCurrency != "USD" {
		t.Fatalf("default currency: %+v", loose)
	}
}

func TestSitrepCarriesSupplyAndItsWarnings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	lands := clock.AddDate(0, 0, 5).Format("2006-01-02")
	var work domain.Front
	deployments := &fakeDeployments{}
	manage, _, l := newLogisticsFixture(t, &clock, deployments)
	sitreps := NewSitreps(manage, nil, time.UTC, true).WithLogistics(l)
	sitreps.now = manage.now

	// Nothing reported: the sitrep and brief are what they were.
	work, _ = manage.CreateFront(ctx, "Work")
	ship, _ := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship v2", FrontID: &work.ID})
	due := clock.Add(24 * time.Hour)
	for _, text := range []string{"Write the tests", "Fix the build", "Draft the post"} {
		c, err := manage.orders.Create(ctx, comusecase.CreateInput{Text: text, TargetID: &ship.ID, DueAt: &due})
		if err != nil {
			t.Fatal(err)
		}
		if c.Status != comdomain.StatusOpen {
			t.Fatalf("a hand-made order is open: %+v", c)
		}
	}
	brief, err := sitreps.Situation(ctx)
	if err != nil || strings.Contains(brief, "Supply, as the person reports it") {
		t.Fatalf("no supply reported, none in the brief: %v\n%s", err, brief)
	}

	// One sortie: three orders due tomorrow need more than it supplies.
	if _, err := l.LogSortie(ctx, SortieInput{FrontID: work.ID}); err != nil {
		t.Fatal(err)
	}
	deployments.list = []domain.Deployment{{PlanID: uuid.New(), FrontID: work.ID, Destination: "Lisbon", ArriveOn: lands, Currency: "EUR", Cost: 900, CriticalOpen: 1}}
	s, err := sitreps.Sitrep(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.Logistics == nil || !s.Logistics.Fronts[0].Outrun {
		t.Fatalf("the sitrep reads supply: %+v", s.Logistics)
	}
	for _, kind := range []domain.ReadingKind{domain.ReadingOutrun, domain.ReadingDeployment} {
		found := false
		for _, r := range s.Readings {
			found = found || r.Kind == kind
		}
		if !found {
			t.Fatalf("missing %s: %+v", kind, s.Readings)
		}
	}
	brief, _ = sitreps.Situation(ctx)
	if !strings.Contains(brief, "- Work: ") || !strings.Contains(brief, "deployment to Lisbon") {
		t.Fatalf("the brief carries supply:\n%s", brief)
	}

	// A planner that cannot be read drops the stays, not the supply.
	deployments.err = errors.New("planner down")
	s, err = sitreps.Sitrep(ctx)
	if err != nil || s.Logistics == nil || len(s.Logistics.Fronts[0].Deployments) != 0 || !s.Logistics.Fronts[0].Outrun {
		t.Fatalf("deployments failing: %+v %v", s.Logistics, err)
	}
}

func fptr(v float64) *float64 { return &v }
