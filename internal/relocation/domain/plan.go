// Package domain holds the relocation model: a stay somewhere, everything that
// stay needs, and what each of those things is likely to cost.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// Housing determines how much of a home you have to supply yourself, which is
// the single biggest driver of the setup list.
type Housing string

const (
	HousingHotel       Housing = "hotel"
	HousingServiced    Housing = "serviced"  // aparthotel: linens, kitchen basics, cleaning
	HousingFurnished   Housing = "furnished" // furniture, rarely bedding or kitchen basics
	HousingUnfurnished Housing = "unfurnished"
	HousingShared      Housing = "shared" // room in a flat; kitchen exists, bathroom shared
)

type Climate string

const (
	ClimateTropical  Climate = "tropical"
	ClimateTemperate Climate = "temperate"
	ClimateCold      Climate = "cold"
	ClimateArid      Climate = "arid"
)

// BudgetStyle scales what counts as "needed". Frugal buys the cheapest thing
// that works; comfortable buys the one you will not resent after a month.
type BudgetStyle string

const (
	BudgetFrugal      BudgetStyle = "frugal"
	BudgetStandard    BudgetStyle = "standard"
	BudgetComfortable BudgetStyle = "comfortable"
)

type PlanStatus string

const (
	PlanDraft     PlanStatus = "draft"
	PlanActive    PlanStatus = "active"
	PlanDone      PlanStatus = "done"
	PlanAbandoned PlanStatus = "abandoned"
)

type Plan struct {
	ID          uuid.UUID
	Destination string // free text as you would say it: "Tbilisi, Georgia"
	CountryCode string // ISO-3166 alpha-2 when known; drives visa and legal pitfalls
	ArriveOn    time.Time
	DepartOn    time.Time
	Nights      int
	PartySize   int
	Housing     Housing
	Climate     Climate
	BudgetStyle BudgetStyle
	Currency    string
	Status      PlanStatus
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time

	Items    []Item
	Pitfalls []PlanPitfall
}

type Category string

const (
	CategoryArrival    Category = "arrival"
	CategoryHousing    Category = "housing"
	CategoryBathroom   Category = "bathroom"
	CategorySkincare   Category = "skincare"
	CategoryKitchen    Category = "kitchen"
	CategoryBedding    Category = "bedding"
	CategoryLaundry    Category = "laundry"
	CategoryWorkspace  Category = "workspace"
	CategoryHealth     Category = "health"
	CategoryConnect    Category = "connectivity"
	CategoryAdmin      Category = "admin"
	CategoryClimateKit Category = "climate"
	CategoryCleaning   Category = "cleaning"
	CategoryExit       Category = "exit"
)

// ItemStatus tracks a line from "you will need this" to resolved. "have" means
// you are carrying it already, which is the difference between a useful list and
// a generic one.
type ItemStatus string

const (
	ItemNeeded  ItemStatus = "needed"
	ItemHave    ItemStatus = "have"
	ItemBought  ItemStatus = "bought"
	ItemSkipped ItemStatus = "skipped"
)

// CostSource records where a number came from. This is load-bearing: an LLM will
// produce confident, wrong prices for a specific city, and a budget that cannot
// tell you which figures are guesses is worse than one with no figures at all.
type CostSource string

const (
	// CostFromCatalog is the shipped anchor price — a rough global baseline.
	CostFromCatalog CostSource = "catalog"
	// CostFromModel is a localised estimate. Treat as an order of magnitude.
	CostFromModel CostSource = "model"
	// CostFromHistory is a price you actually paid in this destination before.
	CostFromHistory CostSource = "history"
	// CostFromUser is a number you entered or confirmed.
	CostFromUser CostSource = "user"
)

// Trustworthy reports whether a figure came from reality rather than a guess.
func (s CostSource) Trustworthy() bool {
	return s == CostFromUser || s == CostFromHistory
}

// AnchorCurrency is the currency the catalogue's baseline prices are expressed
// in. A line keeps it until a pricing pass or a price you enter replaces it.
const AnchorCurrency = "USD"

type Item struct {
	ID        uuid.UUID
	PlanID    uuid.UUID
	CatalogID string // empty for a line you added yourself
	Category  Category
	Name      string
	Quantity  float64
	Unit      string
	UnitCost  *float64 // nil means genuinely unknown, which is not the same as free
	Currency  string
	Source    CostSource
	// Confidence is the model's own estimate, 0-1, and only meaningful when
	// Source is CostFromModel.
	Confidence        *float64
	Status            ItemStatus
	Note              string
	CommonlyForgotten bool
	SortOrder         int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// LineTotal is nil when the unit cost is unknown, so an unpriced line cannot
// quietly contribute zero to a budget.
func (i Item) LineTotal() *float64 {
	if i.UnitCost == nil || i.Status == ItemHave || i.Status == ItemSkipped {
		return nil
	}
	total := *i.UnitCost * i.Quantity
	return &total
}

type Severity string

const (
	SeverityCritical Severity = "critical" // can end the trip or is illegal
	SeverityCostly   Severity = "costly"   // money you will not get back
	SeverityAnnoying Severity = "annoying" // ruins a week, fixable
)

type PlanPitfall struct {
	ID             uuid.UUID
	PlanID         uuid.UUID
	PitfallID      string
	Title          string
	Body           string
	Severity       Severity
	Action         string
	AcknowledgedAt *time.Time
	CreatedAt      time.Time
}
