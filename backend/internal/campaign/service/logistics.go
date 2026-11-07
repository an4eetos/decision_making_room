package service

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

// Logistics thresholds. Arithmetic, like the readings: the staff argues from
// the same numbers every time, and a check-in can name them without a model.
const (
	// SortieRecordDays is how far back sorties are read: the record the rate
	// per order is measured over.
	SortieRecordDays = 28
	// EnergyRecordDays is how much of the energy record is read.
	EnergyRecordDays = 7

	// supplyWindowDays is what hours per week are measured over: two weeks,
	// so a day off moves the rate a little rather than by half. No penalty
	// for a day off.
	supplyWindowDays = 14
	// supplyMinWindowDays is the shortest window, for a record younger than
	// two weeks: measured over what there is, at least a week of it.
	supplyMinWindowDays = 7
	// rateMinOrders is how many orders carried out it takes before a front's
	// own rate replaces the one-sortie-an-order estimate.
	rateMinOrders = 3
	// A measured rate is held between a quarter of an hour and a working day
	// an order, so one odd week cannot make the forecast absurd.
	minHoursPerOrder = 0.25
	maxHoursPerOrder = 8
	// dueSoonDays is the horizon an order counts as due soon within.
	dueSoonDays = 7
	// energyLookback is how many of the latest evenings low energy is
	// judged over; most of them low is the reading.
	energyLookback = 3
	// deploymentLeadDays is how far ahead a landing is read.
	deploymentLeadDays = 14
	// deploymentEstimated is the estimated share of a deployment's cost
	// worth naming before it lands.
	deploymentEstimated = 0.5

	defaultSortieMinutes = 25
	defaultStaleAfter    = 14 * 24 * time.Hour
)

// SupplyInput is everything logistics is read from, all of it reported:
// sorties logged, energy tapped, funds and costs set, stays deployed.
type SupplyInput struct {
	Fronts []domain.Front
	Items  []domain.Item
	Orders []comdomain.Commitment
	// Sorties are the last SortieRecordDays of them.
	Sorties []domain.Sortie
	// FirstSortie is the day of the first sortie on record, empty with none.
	FirstSortie string
	// Energy is the last EnergyRecordDays of readings.
	Energy      []domain.EnergyReport
	Deployments []domain.Deployment
	// SortieMinutes is the default block, and the estimate an order costs
	// where a front's own record is too thin to measure.
	SortieMinutes int
	// StaleAfter is how long an open order can go untouched before it goes
	// stale: the horizon a front's backlog has to clear within.
	StaleAfter time.Duration
	// Now is in the person's timezone; days are its days.
	Now time.Time
}

// Supply reads what supplies each active front: hours a week from its
// sorties against the hours its open orders need, and its funds against what
// it will cost. The forecast is deliberately plain — open orders times what
// an order costs here, against the hours the front has been getting — so it
// can be argued with: every number in it is one the person reported.
func Supply(in SupplyInput) domain.Logistics {
	loc := in.Now.Location()
	today := midnight(in.Now)
	minutes := in.SortieMinutes
	if minutes <= 0 {
		minutes = defaultSortieMinutes
	}
	stale := in.StaleAfter
	if stale <= 0 {
		stale = defaultStaleAfter
	}

	l := domain.Logistics{
		SortieMinutes: minutes,
		WindowDays:    supplyWindowDays,
		StaleDays:     int(math.Round(stale.Hours() / 24)),
		Sorties:       in.FirstSortie != "",
		Fronts:        []domain.FrontSupply{},
	}
	if first, ok := localDay(in.FirstSortie, loc); ok {
		l.WindowDays = clampInt(daysBetween(first, today)+1, supplyMinWindowDays, supplyWindowDays)
	}
	windowStart := today.AddDate(0, 0, -(l.WindowDays - 1))
	recordStart := today.AddDate(0, 0, -(SortieRecordDays - 1))
	// Due before the midnight that ends the seventh day from today.
	dueBy := today.AddDate(0, 0, dueSoonDays+1)
	staleWeeks := stale.Hours() / 24 / 7

	// An order is on the front its target is filed under, the same rule the
	// theatre lays lanes out by.
	frontOf := map[uuid.UUID]uuid.UUID{}
	for _, it := range in.Items {
		if it.Status == domain.StatusProposed || it.Status == domain.StatusDropped || it.FrontID == nil {
			continue
		}
		frontOf[it.ID] = *it.FrontID
	}

	for _, f := range in.Fronts {
		if f.Status != domain.FrontActive {
			continue
		}
		fs := domain.FrontSupply{FrontID: f.ID, Front: f.Name, Deployments: []domain.Deployment{}}

		var windowMin, earlierMin int
		for _, s := range in.Sorties {
			if s.FrontID != f.ID {
				continue
			}
			day, ok := localDay(s.Day, loc)
			if !ok || day.Before(recordStart) || day.After(today) {
				continue
			}
			if day.Before(windowStart) {
				earlierMin += s.Minutes
				continue
			}
			windowMin += s.Minutes
			fs.Sorties++
		}
		perWeek := float64(windowMin) / 60 * 7 / float64(l.WindowDays)

		done := 0
		for _, o := range in.Orders {
			if o.TargetID == nil || frontOf[*o.TargetID] != f.ID {
				continue
			}
			switch o.Status {
			case comdomain.StatusDone:
				if o.ResolvedAt != nil && !o.ResolvedAt.Before(recordStart) {
					done++
				}
			case comdomain.StatusOpen, comdomain.StatusStale:
				fs.OrdersOpen++
				if o.DueAt != nil && o.DueAt.Before(dueBy) {
					fs.OrdersDueSoon++
				}
			}
		}

		perOrder := float64(minutes) / 60
		if recordMin := windowMin + earlierMin; done >= rateMinOrders && recordMin > 0 {
			perOrder = math.Min(math.Max(float64(recordMin)/60/float64(done), minHoursPerOrder), maxHoursPerOrder)
			fs.PerOrderMeasured = true
		}
		dueSoon := float64(fs.OrdersDueSoon) * perOrder
		backlog := float64(fs.OrdersOpen) * perOrder
		var weeks float64
		if perWeek > 0 {
			weeks = backlog / perWeek
		}

		// Time readings need a record: a front is not out of supply for
		// never having logged a sortie, and one that never did is not
		// judged by them. Outrunning is the work exceeding what the supply
		// delivers in time by more than one order, the slack anyone has for
		// one extra block: a single order against a thin week is a stretch,
		// not a front about to collapse.
		fs.Outrun = l.Sorties && perWeek > 0 && fs.OrdersOpen > 0 &&
			(dueSoon > perWeek+perOrder || backlog > perWeek*staleWeeks+perOrder)
		fs.Cut = l.Sorties && windowMin == 0 && earlierMin > 0 && fs.OrdersDueSoon > 0

		fs.HoursPerWeek = round2(perWeek)
		fs.EarlierHours = round2(float64(earlierMin) / 60)
		fs.HoursPerOrder = round2(perOrder)
		fs.DueSoonHours = round2(dueSoon)
		fs.BacklogHours = round2(backlog)
		fs.BacklogWeeks = round2(weeks)

		frontMoney(&fs, f, in.Items, in.Deployments, today, loc)
		l.Fronts = append(l.Fronts, fs)
	}

	l.Energy = readEnergy(in.Energy, today, loc)
	return l
}

// frontMoney sums what the live objectives and deployments on a front will
// cost, in the currency of its funds or else the one carrying most of it.
// Other currencies are named, never converted at a guessed rate.
func frontMoney(fs *domain.FrontSupply, f domain.Front, items []domain.Item, deployments []domain.Deployment, today time.Time, loc *time.Location) {
	costs, estimated := map[string]float64{}, map[string]float64{}
	for _, it := range items {
		if it.Type != domain.TypeObjective || it.Status != domain.StatusActive || it.Cost == nil ||
			it.FrontID == nil || *it.FrontID != f.ID || it.CostCurrency == "" {
			continue
		}
		costs[it.CostCurrency] += *it.Cost
	}
	for _, d := range deployments {
		if d.FrontID != f.ID {
			continue
		}
		if depart, ok := localDay(d.DepartOn, loc); ok && depart.Before(today) {
			continue // the stay is over
		}
		if arrive, ok := localDay(d.ArriveOn, loc); ok {
			n := daysBetween(today, arrive)
			d.LandsIn = &n
		}
		fs.Deployments = append(fs.Deployments, d)
		if d.Cost > 0 && d.Currency != "" {
			costs[d.Currency] += d.Cost
			estimated[d.Currency] += d.Cost * d.EstimatedShare
		}
	}

	currency := ""
	if f.Funds != nil {
		funds := *f.Funds
		fs.Funds, currency = &funds, f.FundsCurrency
	}
	if currency == "" {
		for cur, total := range costs {
			if currency == "" || total > costs[currency] || (total == costs[currency] && cur < currency) {
				currency = cur
			}
		}
	}
	fs.Currency = currency
	fs.Costs = round2(costs[currency])
	fs.CostsEstimated = round2(estimated[currency])
	for cur, total := range costs {
		if cur != currency && total > 0 {
			fs.Unconverted = append(fs.Unconverted, domain.Money{Amount: round2(total), Currency: cur})
		}
	}
	sort.Slice(fs.Unconverted, func(i, j int) bool { return fs.Unconverted[i].Currency < fs.Unconverted[j].Currency })
	fs.Overdrawn = fs.Funds != nil && costs[currency] > *fs.Funds
}

// readEnergy keeps the last week's readings, oldest first, and reads low
// energy as most of the latest few evenings reported low or spent.
func readEnergy(reports []domain.EnergyReport, today time.Time, loc *time.Location) domain.EnergySupply {
	e := domain.EnergySupply{Reports: []domain.EnergyReport{}}
	from := today.AddDate(0, 0, -(EnergyRecordDays - 1))
	for _, r := range reports {
		day, ok := localDay(r.Day, loc)
		if !ok || day.Before(from) || day.After(today) || !domain.ValidEnergy(r.Level) {
			continue
		}
		e.Reports = append(e.Reports, r)
		if day.Equal(today) {
			e.Today = r.Level
		}
	}
	sort.SliceStable(e.Reports, func(i, j int) bool { return e.Reports[i].Day < e.Reports[j].Day })

	last := e.Reports[max(0, len(e.Reports)-energyLookback):]
	low := 0
	for _, r := range last {
		if r.Level <= domain.EnergyLow {
			low++
		}
	}
	e.Low = len(last) >= 2 && low*2 > len(last)
	return e
}

// Supplied attaches the logistics to a sitrep and reads them into its
// readings. On a front whose supply line is outrun or cut, that reading
// takes the place of "overextended": it says the same thing sooner, and
// with the numbers. On a front whose sorties keep up with its orders, the
// count-based "overextended" is dropped: the supply has answered it.
func Supplied(s domain.Sitrep, l domain.Logistics) domain.Sitrep {
	s.Logistics = &l
	s.Readings = readings(s)
	return s
}

// supplyOf indexes a sitrep's front supply lines, or returns nil when
// nothing supplies anything yet, so the readings stay as they were.
func supplyOf(s domain.Sitrep) map[uuid.UUID]*domain.FrontSupply {
	if s.Logistics == nil || !s.Logistics.InUse() {
		return nil
	}
	out := map[uuid.UUID]*domain.FrontSupply{}
	for i := range s.Logistics.Fronts {
		out[s.Logistics.Fronts[i].FrontID] = &s.Logistics.Fronts[i]
	}
	return out
}

func outrunText(f domain.FrontSupply, l domain.Logistics) string {
	rate := hoursText(f.HoursPerOrder) + " an order"
	if f.PerOrderMeasured {
		rate += " (its own rate)"
	} else {
		rate += " (est: one sortie an order)"
	}
	getting := hoursText(f.HoursPerWeek) + " a week"
	if f.DueSoonHours > f.HoursPerWeek+f.HoursPerOrder {
		verb := "need"
		if f.OrdersDueSoon == 1 {
			verb = "needs"
		}
		return fmt.Sprintf("%s is outrunning its supply: %d order%s due within a week %s about %s at %s, and it has been getting %s.",
			f.Front, f.OrdersDueSoon, plural(f.OrdersDueSoon), verb, hoursText(f.DueSoonHours), rate, getting)
	}
	return fmt.Sprintf("%s is outrunning its supply: %d open orders are about %s at %s, %s weeks at the %s it has been getting, and an order goes stale after %d days untouched.",
		f.Front, f.OrdersOpen, hoursText(f.BacklogHours), rate, trimFloat(f.BacklogWeeks, 1), getting, l.StaleDays)
}

func cutText(f domain.FrontSupply, l domain.Logistics) string {
	verb := "fall"
	if f.OrdersDueSoon == 1 {
		verb = "falls"
	}
	return fmt.Sprintf("%s has lost its supply: no sorties in the last %d days after %s in the weeks before, and %d order%s %s due within a week.",
		f.Front, l.WindowDays, hoursText(f.EarlierHours), f.OrdersDueSoon, plural(f.OrdersDueSoon), verb)
}

func overdrawnText(f domain.FrontSupply) string {
	est := ""
	if f.CostsEstimated > 0 && f.Costs > 0 {
		est = fmt.Sprintf(", %d%% of the costs an estimate", int(math.Round(f.CostsEstimated/f.Costs*100)))
	}
	return fmt.Sprintf("%s is overdrawn: its costs come to %s against %s of funds%s.",
		f.Front, moneyText(f.Costs, f.Currency), moneyText(*f.Funds, f.Currency), est)
}

// landing is a deployment arriving within the lead time with something
// still outstanding before it does.
func landing(d domain.Deployment) bool {
	if d.LandsIn == nil || *d.LandsIn < 0 || *d.LandsIn > deploymentLeadDays {
		return false
	}
	return d.CriticalOpen > 0 || d.ItemsNeeded > 0 || d.EstimatedShare >= deploymentEstimated
}

func deploymentText(d domain.Deployment, front string) string {
	var parts []string
	if d.CriticalOpen > 0 {
		parts = append(parts, fmt.Sprintf("%d critical pitfall%s not handled", d.CriticalOpen, plural(d.CriticalOpen)))
	}
	if d.ItemsNeeded > 0 {
		parts = append(parts, fmt.Sprintf("%d item%s still needed", d.ItemsNeeded, plural(d.ItemsNeeded)))
	}
	if d.EstimatedShare >= deploymentEstimated {
		parts = append(parts, fmt.Sprintf("%d%% of its cost an estimate", int(math.Round(d.EstimatedShare*100))))
	}
	return fmt.Sprintf("The deployment to %s (%s) lands %s: %s.", d.Destination, front, landsWhen(*d.LandsIn), strings.Join(parts, ", "))
}

func landsWhen(days int) string {
	switch days {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	}
	return fmt.Sprintf("in %d days", days)
}

func energyText(e domain.EnergySupply) string {
	last := e.Reports[max(0, len(e.Reports)-energyLookback):]
	low := 0
	days := make([]string, 0, len(last))
	for _, r := range last {
		if r.Level <= domain.EnergyLow {
			low++
		}
		days = append(days, fmt.Sprintf("%s %s", weekday(r.Day), domain.EnergyLabel(r.Level)))
	}
	share := fmt.Sprintf("%d of the last %d evenings reported were", low, len(last))
	switch {
	case low == len(last) && low == 2:
		share = "both of the last two evenings reported were"
	case low == len(last):
		share = fmt.Sprintf("all of the last %d evenings reported were", low)
	}
	return fmt.Sprintf("Energy has run low: %s low or spent (%s). Supply is thinner than the hours say: cut orders before adding any.",
		share, strings.Join(days, ", "))
}

// supplyHead introduces logistics to a model.
const supplyHead = `Supply, as the person reports it: hours from the focus blocks (sorties) they log against a front, energy from one tap each evening (1 spent to 5 sharp), money from the funds they set and the costs on a front. All of it reported, none inferred; "est" is a figure nobody confirmed. Weigh orders against it: a front outrunning its supply needs orders cut or supply moved, not more orders.`

// writeSupply adds the supply lines to a brief. Nothing when nothing
// supplies anything yet, so the brief is what it was before logistics.
func writeSupply(b *strings.Builder, l *domain.Logistics) {
	if l == nil || !l.InUse() {
		return
	}
	b.WriteString("\n")
	b.WriteString(supplyHead)
	b.WriteString("\n")
	for _, f := range l.Fronts {
		if line := supplyLine(f, *l); line != "" {
			fmt.Fprintf(b, "- %s: %s\n", f.Front, line)
		}
	}
	if len(l.Energy.Reports) > 0 {
		days := make([]string, 0, len(l.Energy.Reports))
		for _, r := range l.Energy.Reports {
			days = append(days, fmt.Sprintf("%s %d", weekday(r.Day), r.Level))
		}
		fmt.Fprintf(b, "- Energy, last %d days: %s\n", EnergyRecordDays, strings.Join(days, ", "))
	}
}

func supplyLine(f domain.FrontSupply, l domain.Logistics) string {
	var parts []string
	if l.Sorties {
		if f.Sorties > 0 {
			parts = append(parts, fmt.Sprintf("%s a week over the last %d days (%d sortie%s)",
				hoursText(f.HoursPerWeek), l.WindowDays, f.Sorties, plural(f.Sorties)))
		} else {
			parts = append(parts, fmt.Sprintf("no sorties in the last %d days", l.WindowDays))
		}
		if f.OrdersOpen > 0 {
			rate := "est"
			if f.PerOrderMeasured {
				rate = "its own rate"
			}
			p := fmt.Sprintf("%d order%s open ≈ %s at %s an order (%s)",
				f.OrdersOpen, plural(f.OrdersOpen), hoursText(f.BacklogHours), hoursText(f.HoursPerOrder), rate)
			if f.OrdersDueSoon > 0 {
				p += fmt.Sprintf(", %d due within a week", f.OrdersDueSoon)
			}
			parts = append(parts, p)
		}
	}
	if f.HasMoney() {
		var p string
		if f.Funds != nil {
			p = fmt.Sprintf("funds %s, costs ahead %s", moneyText(*f.Funds, f.Currency), moneyText(f.Costs, f.Currency))
		} else {
			p = fmt.Sprintf("costs ahead %s, no funds set", moneyText(f.Costs, f.Currency))
		}
		if f.CostsEstimated > 0 && f.Costs > 0 {
			p += fmt.Sprintf(" (%d%% est)", int(math.Round(f.CostsEstimated/f.Costs*100)))
		}
		if len(f.Unconverted) > 0 {
			other := make([]string, 0, len(f.Unconverted))
			for _, m := range f.Unconverted {
				other = append(other, moneyText(m.Amount, m.Currency))
			}
			p += ", plus " + strings.Join(other, " and ") + " not converted"
		}
		parts = append(parts, p)
	}
	for _, d := range f.Deployments {
		parts = append(parts, deploymentLine(d))
	}
	return strings.Join(parts, "; ")
}

func deploymentLine(d domain.Deployment) string {
	var head string
	switch {
	case d.LandsIn == nil:
		head = fmt.Sprintf("deployment to %s, no arrival date", d.Destination)
	case *d.LandsIn < 0:
		head = fmt.Sprintf("deployed in %s", d.Destination)
		if d.DepartOn != "" {
			head += " until " + d.DepartOn
		}
	default:
		head = fmt.Sprintf("deployment to %s, lands %s (%s)", d.Destination, landsWhen(*d.LandsIn), d.ArriveOn)
	}
	var parts []string
	if d.Cost > 0 {
		setup := "setup " + moneyText(d.Cost, d.Currency)
		if d.EstimatedShare > 0 {
			setup += fmt.Sprintf(" (%d%% est)", int(math.Round(d.EstimatedShare*100)))
		}
		parts = append(parts, setup)
	}
	if d.Unpriced > 0 {
		parts = append(parts, fmt.Sprintf("%d line%s unpriced", d.Unpriced, plural(d.Unpriced)))
	}
	if d.ItemsNeeded > 0 {
		parts = append(parts, fmt.Sprintf("%d item%s still needed", d.ItemsNeeded, plural(d.ItemsNeeded)))
	}
	if d.CriticalOpen > 0 {
		parts = append(parts, fmt.Sprintf("%d critical pitfall%s not handled", d.CriticalOpen, plural(d.CriticalOpen)))
	}
	if len(parts) == 0 {
		return head
	}
	return head + ": " + strings.Join(parts, ", ")
}

// hoursText says a duration the way a person would: minutes under an hour,
// hours to one decimal above.
func hoursText(h float64) string {
	if h <= 0 {
		return "0 h"
	}
	if minutes := int(math.Round(h * 60)); minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	return trimFloat(h, 1) + " h"
}

func trimFloat(v float64, decimals int) string {
	p := math.Pow(10, float64(decimals))
	return strconv.FormatFloat(math.Round(v*p)/p, 'f', -1, 64)
}

// moneyText is a whole amount with thousands separators and its currency.
func moneyText(v float64, currency string) string {
	n := int64(math.Round(v))
	neg := n < 0
	if neg {
		n = -n
	}
	digits := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if neg {
		out = "-" + out
	}
	if currency == "" {
		return out
	}
	return out + " " + currency
}

func weekday(day string) string {
	d, err := time.Parse("2006-01-02", day)
	if err != nil {
		return day
	}
	return d.Format("Mon")
}

func midnight(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func localDay(day string, loc *time.Location) (time.Time, bool) {
	if day == "" {
		return time.Time{}, false
	}
	d, err := time.ParseInLocation("2006-01-02", day, loc)
	return d, err == nil
}

// daysBetween counts calendar days from a to b, both local midnights;
// rounding absorbs a daylight-saving hour.
func daysBetween(a, b time.Time) int {
	return int(math.Round(b.Sub(a).Hours() / 24))
}

func clampInt(v, lo, hi int) int {
	return min(max(v, lo), hi)
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
