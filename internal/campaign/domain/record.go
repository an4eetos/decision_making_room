package domain

import (
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
)

// Phase is where a quarter is in its life. It is not advanced when the
// calendar turns; you move it.
type Phase string

const (
	PhasePlanning   Phase = "planning"
	PhaseOperations Phase = "operations"
	PhaseReview     Phase = "review"
	PhaseClosed     Phase = "closed"
)

// Valid reports a phase the record knows.
func (p Phase) Valid() bool {
	switch p {
	case PhasePlanning, PhaseOperations, PhaseReview, PhaseClosed:
		return true
	default:
		return false
	}
}

// Campaign is one quarter: the objectives you assigned to it, the predictions
// filed on it, and, once closed, the commendations and calibration sealed
// with it.
type Campaign struct {
	ID       uuid.UUID
	Quarter  string
	Name     string
	Phase    Phase
	Note     string
	Review   string
	ReviewAt *time.Time
	OpenedAt time.Time
	ClosedAt *time.Time
	// Calibration is the Brier score sealed on close. Nil while the quarter
	// is open, and nil on close when fewer than five predictions were scored.
	Calibration  *float64
	CalibrationN int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Chance is how likely you said a failure was. It is yours; nothing infers it.
type Chance string

const (
	ChanceNone   Chance = ""
	ChanceLong   Chance = "long"   // 0.2
	ChanceEven   Chance = "even"   // 0.5
	ChanceLikely Chance = "likely" // 0.8
)

// Probability is the stated chance as a probability. The second result is
// false when none was stated.
func (c Chance) Probability() (float64, bool) {
	switch c {
	case ChanceLong:
		return 0.2, true
	case ChanceEven:
		return 0.5, true
	case ChanceLikely:
		return 0.8, true
	default:
		return 0, false
	}
}

// Valid reports a chance the record accepts, including none.
func (c Chance) Valid() bool {
	switch c {
	case ChanceNone, ChanceLong, ChanceEven, ChanceLikely:
		return true
	default:
		return false
	}
}

// Outcome is what you scored a prediction as. Empty until you say.
type Outcome string

const (
	OutcomeNone   Outcome = ""
	OutcomeCalled Outcome = "called"
	OutcomeMissed Outcome = "missed"
)

// Valid reports an outcome the record accepts, including none.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeNone, OutcomeCalled, OutcomeMissed:
		return true
	default:
		return false
	}
}

// PredictionStatus is whether a prediction is on the record.
type PredictionStatus string

const (
	PredictionProposed PredictionStatus = "proposed"
	PredictionKept     PredictionStatus = "kept"
	PredictionDropped  PredictionStatus = "dropped"
)

// Live reports a prediction that is still on the record or waiting to be.
func (s PredictionStatus) Live() bool {
	return s == PredictionProposed || s == PredictionKept
}

// Prediction is one failure a pre-mortem named, waiting for you to keep it
// and, later, to score it.
type Prediction struct {
	ID          uuid.UUID
	CampaignID  *uuid.UUID
	ObjectiveID *uuid.UUID
	Text        string
	Warning     string
	Status      PredictionStatus
	Chance      Chance
	Outcome     Outcome
	ScoredAt    *time.Time
	Source      string
	SessionID   *uuid.UUID
	MessageID   *uuid.UUID
	Fingerprint string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CommendationKind is what the line is for. There is no score attached.
type CommendationKind string

const (
	CommendationTaken      CommendationKind = "taken"
	CommendationWithdrawal CommendationKind = "withdrawal"
	CommendationCalled     CommendationKind = "called"
)

// Commendation is a line on the quarter: an objective taken, an honest
// withdrawal, or a pre-mortem that named the failure that showed. Not a point.
type Commendation struct {
	ID         uuid.UUID
	CampaignID uuid.UUID
	Kind       CommendationKind
	Text       string
	RefID      *uuid.UUID
	CreatedAt  time.Time
}

// Bucket is one stated chance and how often that failure showed.
type Bucket struct {
	Chance Chance
	Called int
	Scored int
}

// Calibration is the Brier score over predictions that have both a stated
// chance and an outcome. Score is nil until five of them exist: a shorter
// record is shown as buckets, not as a number.
type Calibration struct {
	Score   *float64
	N       int
	Buckets []Bucket
}

// RecordNote is the one quarter a sitrep may mention. Nil fields mean the
// arithmetic has nothing to say.
type RecordNote struct {
	Name        string
	Quarter     string
	Phase       Phase
	Unscored    int
	Calibration Calibration
}

// CalibrationMin is how many scored predictions a number needs before it is
// worth watching. Below it, the buckets are the record.
const CalibrationMin = 5

var quarterKey = regexp.MustCompile(`^(\d{4})-Q([1-4])$`)

// QuarterOf is the calendar quarter of t, as "2026-Q4".
func QuarterOf(t time.Time) string {
	q := (int(t.Month())-1)/3 + 1
	return fmt.Sprintf("%d-Q%d", t.Year(), q)
}

// ValidQuarter reports a key of the form 2026-Q4.
func ValidQuarter(key string) bool {
	return quarterKey.MatchString(key)
}

// QuarterLabel renders a key as "2026 Q4". An invalid key is returned as is.
func QuarterLabel(key string) string {
	m := quarterKey.FindStringSubmatch(key)
	if m == nil {
		return key
	}
	return m[1] + " Q" + m[2]
}

// QuarterLastDay is the last calendar day of the quarter, YYYY-MM-DD.
// The second result is false when the key is not a quarter.
func QuarterLastDay(key string) (string, bool) {
	m := quarterKey.FindStringSubmatch(key)
	if m == nil {
		return "", false
	}
	var year, q int
	if _, err := fmt.Sscanf(key, "%d-Q%d", &year, &q); err != nil {
		return "", false
	}
	last := time.Date(year, time.Month(q*3)+1, 0, 0, 0, 0, 0, time.UTC)
	return last.Format("2006-01-02"), true
}
