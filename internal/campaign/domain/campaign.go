// Package domain models the campaign: fronts, and on them the objectives you
// are trying to take, the opposition dug in between you and them, and the fog —
// what you do not know yet.
//
// It is a view of what you already wrote, not a second app to maintain. Every
// item extracted from conversation arrives as a proposal and joins the map only
// if you keep it.
package domain

import (
	"time"

	"github.com/google/uuid"
)

// FrontStatus is whether a front is still being fought on.
type FrontStatus string

const (
	FrontActive FrontStatus = "active"
	// FrontWithdrawn is a closed front. Its items stay on the record.
	FrontWithdrawn FrontStatus = "withdrawn"
)

// Front is an area of life or work. You create them; extraction never does.
type Front struct {
	ID        uuid.UUID
	Name      string
	Status    FrontStatus
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// StarterFronts are offered on an empty campaign, one click each. They are
// suggestions for a blank page, not a taxonomy anyone has to adopt.
var StarterFronts = []string{"Work", "Health", "Money", "People"}

// Type is what an item is on the map.
type Type string

const (
	// TypeObjective is a position to take: a goal.
	TypeObjective Type = "objective"
	// TypeObstacle is opposition: a problem, blocker or fear dug in between you
	// and an objective.
	TypeObstacle Type = "obstacle"
	// TypeUnknown is fog of war: a question whose answer would change what you
	// do, lifted by reconnaissance.
	TypeUnknown Type = "unknown"
)

func (t Type) Valid() bool {
	return t == TypeObjective || t == TypeObstacle || t == TypeUnknown
}

// Status is where an item is in its life.
type Status string

const (
	// StatusProposed is extracted and waiting for you to keep or drop it.
	StatusProposed Status = "proposed"
	// StatusActive is on the map.
	StatusActive Status = "active"
	// StatusResolved is an objective taken, an obstacle cleared, or an unknown
	// lifted.
	StatusResolved Status = "resolved"
	// StatusWithdrawn is an objective given up on purpose. A legitimate order,
	// not a loss, and the staff will recommend it when it is the right one.
	StatusWithdrawn Status = "withdrawn"
	// StatusDropped is a proposal you rejected, or an item you took off the map
	// because it was never real.
	StatusDropped Status = "dropped"
)

func (s Status) Valid() bool {
	switch s {
	case StatusProposed, StatusActive, StatusResolved, StatusWithdrawn, StatusDropped:
		return true
	default:
		return false
	}
}

// Live reports whether the item is on the map or waiting to be.
func (s Status) Live() bool { return s == StatusProposed || s == StatusActive }

// Kind is the kind of stuck an obstacle is. The five come from the Stalled
// mode, and they need opposite treatments: telling someone to just start when
// they are blocked on an input they have not chased wastes a day.
type Kind string

const (
	KindUndefined Kind = "undefined" // the next step is not defined
	KindWaiting   Kind = "waiting"   // waiting on input that has not been chased
	KindFear      Kind = "fear"      // afraid of what the result will show
	KindTooBig    Kind = "too_big"   // too large to hold in one sitting
	KindUnwanted  Kind = "unwanted"  // you do not actually want the outcome
)

// Kinds lists every kind, in the order the Stalled mode names them.
var Kinds = []Kind{KindUndefined, KindWaiting, KindFear, KindTooBig, KindUnwanted}

func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Strength is how dug in an obstacle is: an outpost, a dug-in position, or a
// fortress. It stays an estimate until you confirm it.
const (
	StrengthOutpost  = 1
	StrengthDugIn    = 2
	StrengthFortress = 3
)

// ValidStrength reports a strength on the scale.
func ValidStrength(s int) bool { return s >= StrengthOutpost && s <= StrengthFortress }

// Source is where an item came from.
type Source string

const (
	SourceManual        Source = "manual"
	SourceChat          Source = "chat"
	SourceInterrogation Source = "interrogation"
)

// Item is one objective, obstacle or unknown.
type Item struct {
	ID   uuid.UUID
	Type Type
	// FrontID is the front it sits on; nil is unassigned.
	FrontID *uuid.UUID
	// ObjectiveID is the objective an obstacle stands in front of, or an unknown
	// bears on.
	ObjectiveID *uuid.UUID
	Text        string
	Status      Status

	// Obstacles only.
	Kind              Kind
	Strength          int
	StrengthConfirmed bool

	// Unknowns only: what reconnaissance found.
	Answer string

	DueAt       *time.Time
	Source      Source
	SessionID   *uuid.UUID
	MessageID   *uuid.UUID
	Confidence  float64
	Fingerprint string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ResolvedAt  *time.Time
}
