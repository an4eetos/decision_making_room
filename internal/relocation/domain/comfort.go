package domain

// ComfortDimension is one part of daily life a stay can fail at. Scoring them
// separately is what lets the planner say "sleep is at 20 because there is no
// pillow" instead of handing you one number you cannot act on.
type ComfortDimension string

const (
	ComfortHygiene   ComfortDimension = "hygiene"
	ComfortSleep     ComfortDimension = "sleep"
	ComfortFood      ComfortDimension = "food"
	ComfortHome      ComfortDimension = "home" // laundry, cleaning, somewhere to put things
	ComfortWork      ComfortDimension = "work"
	ComfortHealth    ComfortDimension = "health"
	ComfortClimate   ComfortDimension = "climate"
	ComfortConnected ComfortDimension = "connected" // power, data, money
)

// ComfortDimensions is the display order.
var ComfortDimensions = []ComfortDimension{
	ComfortHygiene, ComfortSleep, ComfortFood, ComfortHome,
	ComfortWork, ComfortHealth, ComfortClimate, ComfortConnected,
}

// ComfortWeight is how much a missing item hurts. Critical is not "very
// important": it is the towel, the toothbrush, the sheets — things whose absence
// makes the rest of the list irrelevant, however complete it is.
type ComfortWeight string

const (
	WeightCritical  ComfortWeight = "critical"
	WeightImportant ComfortWeight = "important"
	WeightNice      ComfortWeight = "nice"
)

// Points is the item's share of its dimension's coverage.
func (w ComfortWeight) Points() float64 {
	switch w {
	case WeightCritical:
		return 5
	case WeightImportant:
		return 3
	case WeightNice:
		return 1
	default:
		return 0
	}
}

// ComfortRule says which part of daily life an item serves and how badly its
// absence hurts. Items without one — a visa check, cancelling subscriptions —
// matter, but not to how livable the place feels, and are left out of the score.
type ComfortRule struct {
	Dimension ComfortDimension `yaml:"dimension"`
	Weight    ComfortWeight    `yaml:"weight"`
	// Arrival marks what you need on the first night, before any shop is open.
	// It drives a separate reading, because a stay that will be fine in a week
	// can still start with a miserable night.
	Arrival bool `yaml:"arrival"`
}
