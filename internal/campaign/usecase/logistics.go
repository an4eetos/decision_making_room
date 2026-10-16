package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/service"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

// Bounds on what is logged. A sortie is a focus block, not a day; a record
// is kept close to when it happened, or it is memory rather than a log.
const (
	minSortieMinutes = 5
	maxSortieMinutes = 480
	// sortieBackfillDays is how far back a forgotten sortie can be logged:
	// the record the rate is measured over.
	sortieBackfillDays = service.SortieRecordDays
	// energyBackfillDays is how far back a missed evening can be reported.
	energyBackfillDays = 7
	maxSortieNoteRunes = 200
)

// Logistics keeps what supplies the fronts and reads it: sorties flown,
// energy reported, the stays deployed on a front. Nothing in it calls a
// model, and nothing in it is extracted from what you write.
type Logistics struct {
	repo        port.LogisticsRepository
	manage      *Manage
	deployments port.DeploymentSource
	loc         *time.Location
	// sortieMinutes is SORTIE_MINUTES: the default block, and what an order
	// is estimated to cost before a front has a rate of its own.
	sortieMinutes int
	// staleAfter is COMMITMENT_STALE_AFTER: the horizon a backlog has to
	// clear within before its orders go stale.
	staleAfter time.Duration
	now        func() time.Time
}

// NewLogistics takes an optional deployment source: without one, stays are
// simply not on the map.
func NewLogistics(repo port.LogisticsRepository, manage *Manage, deployments port.DeploymentSource, loc *time.Location, sortieMinutes int, staleAfter time.Duration) *Logistics {
	if loc == nil {
		loc = time.Local
	}
	return &Logistics{
		repo: repo, manage: manage, deployments: deployments, loc: loc,
		sortieMinutes: sortieMinutes, staleAfter: staleAfter, now: time.Now,
	}
}

// SortieMinutes is the default length of a sortie.
func (l *Logistics) SortieMinutes() int {
	if l.sortieMinutes <= 0 {
		return 25
	}
	return l.sortieMinutes
}

// Today is the local date the logistics count days by.
func (l *Logistics) Today() string {
	return l.now().In(l.loc).Format("2006-01-02")
}

// SortieInput is one focus block to log. Zero minutes is the default block;
// an empty day is today.
type SortieInput struct {
	FrontID uuid.UUID
	OrderID *uuid.UUID
	Minutes int
	Day     string
	Note    string
}

// LogSortie records a focus block flown on a front.
func (l *Logistics) LogSortie(ctx context.Context, in SortieInput) (domain.Sortie, error) {
	minutes := in.Minutes
	if minutes == 0 {
		minutes = l.SortieMinutes()
	}
	if minutes < minSortieMinutes || minutes > maxSortieMinutes {
		return domain.Sortie{}, fmt.Errorf("a sortie runs %d minutes to %d hours; log a long day as several", minSortieMinutes, maxSortieMinutes/60)
	}
	day, err := l.pastDay(in.Day, sortieBackfillDays, "a sortie")
	if err != nil {
		return domain.Sortie{}, err
	}
	note := strings.TrimSpace(in.Note)
	if len([]rune(note)) > maxSortieNoteRunes {
		return domain.Sortie{}, fmt.Errorf("keep a sortie's note under %d characters", maxSortieNoteRunes)
	}

	front, err := l.manage.repo.GetFront(ctx, in.FrontID)
	if err != nil {
		return domain.Sortie{}, fmt.Errorf("front: %w", err)
	}
	if front.Status != domain.FrontActive {
		return domain.Sortie{}, fmt.Errorf("that front is withdrawn; restore it first")
	}
	if in.OrderID != nil {
		if l.manage.orders == nil {
			return domain.Sortie{}, fmt.Errorf("order: %w", port.ErrNotFound)
		}
		order, err := l.manage.orders.Get(ctx, *in.OrderID)
		if err != nil {
			return domain.Sortie{}, fmt.Errorf("order: %w", err)
		}
		if order.Status == comdomain.StatusProposed || order.Status == comdomain.StatusDropped {
			return domain.Sortie{}, fmt.Errorf("a sortie is flown on an order you kept")
		}
	}

	return l.repo.CreateSortie(ctx, domain.Sortie{
		FrontID: front.ID, OrderID: in.OrderID, Minutes: minutes, Day: day, Note: note,
	})
}

// DeleteSortie takes a sortie off the record: logged by mistake, or twice.
func (l *Logistics) DeleteSortie(ctx context.Context, id uuid.UUID) error {
	return l.repo.DeleteSortie(ctx, id)
}

// Sorties lists the sorties of the last days, latest first.
func (l *Logistics) Sorties(ctx context.Context, days int) ([]domain.Sortie, error) {
	if days <= 0 || days > service.SortieRecordDays {
		days = service.SortieRecordDays
	}
	return l.repo.ListSorties(ctx, l.since(days))
}

// ReportEnergy records a day's energy, one tap, replacing a tap already made
// that day. An empty day is today; the evening check-in passes its own, so a
// tap after midnight still lands on the evening it was about.
func (l *Logistics) ReportEnergy(ctx context.Context, day string, level int) (domain.EnergyReport, error) {
	if !domain.ValidEnergy(level) {
		return domain.EnergyReport{}, fmt.Errorf("energy runs from %d (spent) to %d (sharp)", domain.EnergySpent, domain.EnergySharp)
	}
	d, err := l.pastDay(day, energyBackfillDays, "energy")
	if err != nil {
		return domain.EnergyReport{}, err
	}
	return l.repo.SaveEnergy(ctx, domain.EnergyReport{Day: d, Level: level})
}

// ClearEnergy takes back a day's tap.
func (l *Logistics) ClearEnergy(ctx context.Context, day string) error {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return errors.New("day must be YYYY-MM-DD")
	}
	return l.repo.DeleteEnergy(ctx, day)
}

// Energy lists the readings of the last days, oldest first.
func (l *Logistics) Energy(ctx context.Context, days int) ([]domain.EnergyReport, error) {
	if days <= 0 || days > 60 {
		days = service.EnergyRecordDays
	}
	return l.repo.ListEnergy(ctx, l.since(days))
}

// Read reads the supply lines for a map as it stands. A deployment source
// that fails is logged and left out: the stays are part of the supply, not
// a reason to lose the rest of it.
func (l *Logistics) Read(ctx context.Context, m Map, now time.Time) (domain.Logistics, error) {
	now = now.In(l.loc)
	sorties, err := l.repo.ListSorties(ctx, dayBefore(now, service.SortieRecordDays))
	if err != nil {
		return domain.Logistics{}, err
	}
	first, err := l.repo.FirstSortieDay(ctx)
	if err != nil {
		return domain.Logistics{}, err
	}
	energy, err := l.repo.ListEnergy(ctx, dayBefore(now, service.EnergyRecordDays))
	if err != nil {
		return domain.Logistics{}, err
	}
	var deployments []domain.Deployment
	if l.deployments != nil {
		if deployments, err = l.deployments.Deployments(ctx); err != nil {
			log.Printf("campaign: reading deployments: %v", err)
			deployments = nil
		}
	}
	return service.Supply(service.SupplyInput{
		Fronts: m.Fronts, Items: m.Items, Orders: m.Orders,
		Sorties: sorties, FirstSortie: first, Energy: energy, Deployments: deployments,
		SortieMinutes: l.SortieMinutes(), StaleAfter: l.staleAfter, Now: now,
	}), nil
}

// pastDay checks a YYYY-MM-DD day is today or within the last few days.
func (l *Logistics) pastDay(day string, back int, what string) (string, error) {
	now := l.now().In(l.loc)
	today := now.Format("2006-01-02")
	day = strings.TrimSpace(day)
	if day == "" {
		return today, nil
	}
	d, err := time.ParseInLocation("2006-01-02", day, l.loc)
	if err != nil {
		return "", errors.New("day must be YYYY-MM-DD")
	}
	if day > today {
		return "", fmt.Errorf("%s is logged once it has happened, not ahead", what)
	}
	if earliest := dayBefore(now, back); d.Format("2006-01-02") < earliest {
		return "", fmt.Errorf("%s can be logged up to %d days back", what, back-1)
	}
	return day, nil
}

func (l *Logistics) since(days int) string {
	return dayBefore(l.now().In(l.loc), days)
}

// dayBefore is the first day of a window of n days ending today.
func dayBefore(now time.Time, n int) string {
	return now.AddDate(0, 0, -(n - 1)).Format("2006-01-02")
}

// ---------- Money ----------

// maxAmount keeps an amount a number a person typed, not an overflow.
const maxAmount = 1e12

// SetFunds sets the money a front can draw on, or with nil clears it. It is
// yours, so it is never marked as an estimate.
func (u *Manage) SetFunds(ctx context.Context, id uuid.UUID, funds *float64, currency string) (domain.Front, error) {
	f, err := u.repo.GetFront(ctx, id)
	if err != nil {
		return domain.Front{}, err
	}
	if funds == nil {
		f.Funds, f.FundsCurrency = nil, ""
		return u.repo.UpdateFront(ctx, f)
	}
	amount, cur, err := cleanMoney(*funds, currency)
	if err != nil {
		return domain.Front{}, err
	}
	f.Funds, f.FundsCurrency = &amount, cur
	return u.repo.UpdateFront(ctx, f)
}

// setCost sets or clears what taking an objective will cost. With no
// currency it takes its front's funds currency, so the costs on a front sum.
func (u *Manage) setCost(ctx context.Context, item *domain.Item, cost *float64, currency string, clear bool) error {
	if item.Type != domain.TypeObjective {
		return fmt.Errorf("only an objective has a cost")
	}
	if clear || cost == nil {
		item.Cost, item.CostCurrency = nil, ""
		return nil
	}
	currency = strings.TrimSpace(currency)
	if currency == "" && item.FrontID != nil {
		if front, err := u.repo.GetFront(ctx, *item.FrontID); err == nil && front.Funds != nil {
			currency = front.FundsCurrency
		}
	}
	if currency == "" {
		currency = "USD"
	}
	amount, cur, err := cleanMoney(*cost, currency)
	if err != nil {
		return err
	}
	item.Cost, item.CostCurrency = &amount, cur
	return nil
}

func cleanMoney(amount float64, currency string) (float64, string, error) {
	if amount < 0 || amount > maxAmount || amount != amount {
		return 0, "", fmt.Errorf("an amount is a number from 0 up")
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if len(cur) != 3 || strings.Trim(cur, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return 0, "", fmt.Errorf("currency is a three-letter code, like EUR or USD")
	}
	return amount, cur, nil
}
