package domain

import (
	"time"

	"github.com/google/uuid"
)

// Logistics is what supplies each front: hours from the sorties you log,
// energy from the one tap at the evening check-in, money from the funds you
// set against a front and the costs drawn on it. Every figure is one you
// reported; none is inferred from what you write, because a supply line
// guessed from your tone is one you would stop trusting.
//
// Like the sitrep it is arithmetic, not judgment. It says when a front's
// orders are outrunning its supply; whether to cut orders or move supply is
// the staff's call.

// Sortie is one focus block flown on a front.
type Sortie struct {
	ID      uuid.UUID `json:"id"`
	FrontID uuid.UUID `json:"front_id"`
	// OrderID is the order it was spent on; nil is the front in general.
	OrderID *uuid.UUID `json:"order_id,omitempty"`
	Minutes int        `json:"minutes"`
	// Day is the local date it was flown, YYYY-MM-DD.
	Day       string    `json:"day"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Energy is reported once a day in one tap, on a five-point scale.
const (
	EnergySpent  = 1
	EnergyLow    = 2
	EnergySteady = 3
	EnergyGood   = 4
	EnergySharp  = 5
)

// ValidEnergy reports a level on the scale.
func ValidEnergy(level int) bool { return level >= EnergySpent && level <= EnergySharp }

// EnergyLabel names a level the way the tap does.
func EnergyLabel(level int) string {
	switch level {
	case EnergySpent:
		return "spent"
	case EnergyLow:
		return "low"
	case EnergySteady:
		return "steady"
	case EnergyGood:
		return "good"
	case EnergySharp:
		return "sharp"
	}
	return ""
}

// EnergyReport is one day's reading.
type EnergyReport struct {
	// Day is the local date it is about, YYYY-MM-DD: the evening's day, even
	// when the tap comes after midnight.
	Day        string    `json:"day"`
	Level      int       `json:"level"`
	ReportedAt time.Time `json:"reported_at"`
}

// Deployment is a relocation plan deployed on a front: a stay as a forward
// base, with its own supply line. Its cost is the plan's setup budget, which
// keeps the planner's honesty: EstimatedShare is how much of it is a
// catalogue anchor or a model's price rather than one you confirmed.
type Deployment struct {
	PlanID      uuid.UUID `json:"plan_id"`
	FrontID     uuid.UUID `json:"front_id"`
	Destination string    `json:"destination"`
	// ArriveOn and DepartOn are YYYY-MM-DD, empty when the plan has none.
	ArriveOn string `json:"arrive_on,omitempty"`
	DepartOn string `json:"depart_on,omitempty"`
	Status   string `json:"status"`

	Currency       string  `json:"currency"`
	Cost           float64 `json:"cost"`
	EstimatedShare float64 `json:"estimated_share"`
	// Unpriced lines contribute nothing to Cost, which is why they are
	// counted: an unknown price is not a free one.
	Unpriced      int `json:"unpriced"`
	ItemsNeeded   int `json:"items_needed"`
	ItemsResolved int `json:"items_resolved"`
	// CriticalOpen counts critical pitfalls not yet handled.
	CriticalOpen int `json:"critical_open"`

	// LandsIn is days until arrival, set when the logistics are read:
	// negative once arrived, nil with no arrival date.
	LandsIn *int `json:"lands_in,omitempty"`
}

// Logistics is the supply read for one moment.
type Logistics struct {
	// SortieMinutes is the default block: what an order is estimated to
	// cost when a front's own record is too thin to measure.
	SortieMinutes int `json:"sortie_minutes"`
	// WindowDays is how many days hours per week are measured over.
	WindowDays int `json:"window_days"`
	// StaleDays is how long an open order goes untouched before it goes
	// stale: the horizon a front's backlog has to clear within.
	StaleDays int `json:"stale_days"`
	// Sorties reports whether any sortie is on record at all. Time readings
	// need one: a front is not out of supply for never having logged any.
	Sorties bool          `json:"sorties"`
	Fronts  []FrontSupply `json:"fronts"`
	Energy  EnergySupply  `json:"energy"`
}

// InUse reports whether anything supplies anything yet. With nothing
// reported, logistics adds nothing to the brief or the readings.
func (l Logistics) InUse() bool {
	if l.Sorties || len(l.Energy.Reports) > 0 {
		return true
	}
	for _, f := range l.Fronts {
		if f.HasMoney() || len(f.Deployments) > 0 {
			return true
		}
	}
	return false
}

// FrontSupply is one front's supply line.
type FrontSupply struct {
	FrontID uuid.UUID `json:"front_id"`
	Front   string    `json:"front"`

	// Sorties flown in the window, and the hours a week they come to.
	Sorties      int     `json:"sorties"`
	HoursPerWeek float64 `json:"hours_per_week"`
	// EarlierHours are the hours flown in the record before the window: a
	// front that had supply and lost it reads differently from one that
	// never had any.
	EarlierHours float64 `json:"earlier_hours"`

	// HoursPerOrder is what an order costs here. Measured is a rate from the
	// front's own record (sortie hours over the orders carried out in the
	// last four weeks); otherwise it is one sortie an order, an estimate.
	HoursPerOrder    float64 `json:"hours_per_order"`
	PerOrderMeasured bool    `json:"per_order_measured"`

	OrdersOpen int `json:"orders_open"`
	// OrdersDueSoon are open orders due within a week, or overdue.
	OrdersDueSoon int     `json:"orders_due_soon"`
	DueSoonHours  float64 `json:"due_soon_hours"`
	BacklogHours  float64 `json:"backlog_hours"`
	// BacklogWeeks is how long the open orders take at this front's supply;
	// zero with no supply to measure.
	BacklogWeeks float64 `json:"backlog_weeks"`

	// Outrun is a front whose orders are outrunning its supply: what falls
	// due within a week needs more hours than it has been getting, or the
	// backlog would take longer to clear than an order stays fresh — by
	// more than one order's worth, the slack of one extra block.
	Outrun bool `json:"outrun"`
	// Cut is a front whose sorties stopped while orders on it fall due.
	Cut bool `json:"cut"`

	// Funds is what the front can draw on, in Currency; nil is none set.
	Funds    *float64 `json:"funds,omitempty"`
	Currency string   `json:"currency,omitempty"`
	// Costs are what the live objectives and deployments on it will cost,
	// in Currency, and CostsEstimated the part of that nobody confirmed.
	Costs          float64 `json:"costs"`
	CostsEstimated float64 `json:"costs_estimated"`
	// Unconverted are the costs in other currencies, left out of Costs and
	// listed by currency. They are never converted at a guessed rate.
	Unconverted []Money `json:"unconverted,omitempty"`
	Overdrawn   bool    `json:"overdrawn"`

	Deployments []Deployment `json:"deployments"`
}

// Money is an amount in one currency.
type Money struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// HasMoney reports whether the front has funds or costs to read.
func (f FrontSupply) HasMoney() bool {
	return f.Funds != nil || f.Costs > 0 || len(f.Unconverted) > 0
}

// EnergySupply is the last week of energy as reported.
type EnergySupply struct {
	// Reports are the last seven days', oldest first.
	Reports []EnergyReport `json:"reports"`
	// Today is today's level, zero when not reported yet.
	Today int `json:"today"`
	// Low is most of the last few evenings reported as low or spent.
	Low bool `json:"low"`
}
