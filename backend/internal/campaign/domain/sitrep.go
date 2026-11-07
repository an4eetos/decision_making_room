package domain

import (
	"time"

	"github.com/google/uuid"
)

// Sitrep is the map read for one moment, the way the staff reads it before
// saying anything: every front with its line and what sits on it, the orders
// aimed at each position, what moved in the last day, and the readings the
// arithmetic alone supports. It is what the generals get before a question,
// what an appraisal starts from, and what a check-in is drawn from.
//
// Deterministic by design. Everything here is a view of the map and
// yesterday's snapshot; judgment (the point of main effort, where to yield)
// is left to a general who reads it.
type Sitrep struct {
	// Day is the local date, YYYY-MM-DD.
	Day     string    `json:"day"`
	TakenAt time.Time `json:"taken_at"`
	// Empty reports a map with no fronts and nothing on it. Everything that
	// reads a sitrep degrades to nothing on an empty one.
	Empty  bool          `json:"empty"`
	Fronts []SitrepFront `json:"fronts"`
	Totals TheatreTotals `json:"totals"`
	// Since is the day the front lines are compared against: yesterday's
	// snapshot, or the last one before today. Empty with no record.
	Since string `json:"since,omitempty"`
	// Moved is what changed in the last twenty-four hours.
	Moved Movement `json:"moved"`
	// Readings are what the numbers say on their own.
	Readings []Reading `json:"readings"`
	// Logistics is what supplies each front, when it was read. Nil leaves
	// the sitrep as it was before supply existed.
	Logistics *Logistics `json:"logistics,omitempty"`
}

// SitrepFront is one front as read.
type SitrepFront struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Line is ground held, 0 to 1, and PriorLine where it stood on Since.
	Line      float64 `json:"line"`
	PriorLine float64 `json:"prior_line"`
	HasPrior  bool    `json:"has_prior"`
	// Quiet is a front with no open orders and a line that has not moved.
	Quiet      bool              `json:"quiet"`
	OrdersOpen int               `json:"orders_open"`
	OrdersDue  int               `json:"orders_due"`
	Objectives []SitrepObjective `json:"objectives"`
	// Loose is the opposition and fog tied to no objective on this front.
	Loose []SitrepPost `json:"loose"`
}

// SitrepObjective is one row: the objective, what stands in front of it, and
// the orders aimed at any of them.
type SitrepObjective struct {
	TheatrePost
	Progress   float64       `json:"progress"`
	Opposition []SitrepPost  `json:"opposition"`
	Fog        []SitrepPost  `json:"fog"`
	Orders     []SitrepOrder `json:"orders"`
}

// SitrepPost is an enemy position or a patch of fog, with the orders aimed at
// it. Scouted is fog with reconnaissance sent.
type SitrepPost struct {
	TheatrePost
	Type    Type          `json:"type"`
	Scouted bool          `json:"scouted,omitempty"`
	Orders  []SitrepOrder `json:"orders"`
}

// SitrepOrder is one order as the staff reads it.
type SitrepOrder struct {
	ID    uuid.UUID `json:"id"`
	Text  string    `json:"text"`
	Recon bool      `json:"recon,omitempty"`
	Done  bool      `json:"done,omitempty"`
	// Due is YYYY-MM-DD; DueToday and Overdue are against the sitrep's day.
	Due      string `json:"due,omitempty"`
	DueToday bool   `json:"due_today,omitempty"`
	Overdue  bool   `json:"overdue,omitempty"`
}

// Movement is what changed in the last twenty-four hours: the record an
// evening sitrep reports as gains and losses.
type Movement struct {
	Taken   []string `json:"taken"`
	Cleared []string `json:"cleared"`
	Lifted  []string `json:"lifted"`
	// Withdrawn objectives are listed as what they are, not hidden among
	// losses: an orderly withdrawal is a legitimate order.
	Withdrawn  []string `json:"withdrawn"`
	OrdersDone []string `json:"orders_done"`
	// Lines are the fronts whose line moved since Since.
	Lines []LineMove `json:"lines"`
}

// Any reports whether anything moved at all.
func (m Movement) Any() bool {
	return len(m.Taken)+len(m.Cleared)+len(m.Lifted)+len(m.Withdrawn)+len(m.OrdersDone)+len(m.Lines) > 0
}

// LineMove is a front line that moved.
type LineMove struct {
	Front string  `json:"front"`
	From  float64 `json:"from"`
	To    float64 `json:"to"`
}

// ReadingKind names what a reading is about.
type ReadingKind string

const (
	// ReadingOverdue is an active objective past its date.
	ReadingOverdue ReadingKind = "overdue"
	// ReadingQuiet is a front with no open orders whose line has not moved.
	ReadingQuiet ReadingKind = "quiet"
	// ReadingOverextended is a front with several orders open and no ground
	// gained since the last snapshot: more committed than it can carry.
	ReadingOverextended ReadingKind = "overextended"
	// ReadingUnscouted is fog with no reconnaissance sent against it.
	ReadingUnscouted ReadingKind = "unscouted"
	// ReadingFortress is a fortress with nothing aimed at it: either go
	// around or commit; sitting in front of it is neither.
	ReadingFortress ReadingKind = "fortress"
	// ReadingUnwanted is opposition typed as an outcome not really wanted,
	// which is a case for withdrawing the objective behind it.
	ReadingUnwanted ReadingKind = "unwanted"
	// ReadingEstimated counts enemy strengths nobody has confirmed.
	ReadingEstimated ReadingKind = "estimated"
	// ReadingAdvanced is the live objective closest to taken: the candidate
	// for the point of main effort, if the staff agrees.
	ReadingAdvanced ReadingKind = "advanced"

	// ReadingOutrun is a front whose orders are outrunning its supply: the
	// overextension warning that comes before the front collapses, rather
	// than after the line has stopped moving.
	ReadingOutrun ReadingKind = "outrun"
	// ReadingCut is a front whose sorties stopped while orders on it fall
	// due: a supply line cut.
	ReadingCut ReadingKind = "cut"
	// ReadingOverdrawn is a front whose costs exceed its funds.
	ReadingOverdrawn ReadingKind = "overdrawn"
	// ReadingDeployment is a deployment landing within two weeks with work
	// still outstanding before it does.
	ReadingDeployment ReadingKind = "deployment"
	// ReadingEnergy is energy reported low on most of the last evenings.
	ReadingEnergy ReadingKind = "energy"

	// ReadingUnscored is a quarter in review whose kept predictions have not
	// all been scored. The debrief asks; it does not decide.
	ReadingUnscored ReadingKind = "unscored"
	// ReadingCalibration is the Brier score, once enough predictions have
	// been scored for the number to be worth watching.
	ReadingCalibration ReadingKind = "calibration"
)

// Reading is one thing the arithmetic says. FrontID and ItemID point at what
// it is about, so a page can link it; either may be nil.
type Reading struct {
	Kind    ReadingKind `json:"kind"`
	Text    string      `json:"text"`
	FrontID *uuid.UUID  `json:"front_id,omitempty"`
	ItemID  *uuid.UUID  `json:"item_id,omitempty"`
}
