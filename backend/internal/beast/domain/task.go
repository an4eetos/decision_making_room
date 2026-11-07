// Package domain models beast mode's board: orders with a clock on them.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	// StatusOpen is on the clock. It leaves only when you report it done.
	StatusOpen Status = "open"
	StatusDone Status = "done"
	// StatusAbandoned is struck off without being done: by you, or by standing
	// down with it still open. The generals call it cowardice.
	StatusAbandoned Status = "abandoned"
)

func (s Status) Resolved() bool { return s == StatusDone || s == StatusAbandoned }

// Minutes bounds. Under a minute is a typo; over four hours is not a crisis.
const (
	MinMinutes = 1
	MaxMinutes = 240
)

// minRearm is the shortest clock a missed deadline is re-armed to. Shorter
// than this and the eruption about the miss would eat the time to fix it.
const minRearm = 5 * time.Minute

type Task struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	// MessageID is the answer that issued the order; nil when added by hand.
	MessageID *uuid.UUID
	// Seq is the task's number within the session, starting at one.
	Seq        int
	Text       string
	Minutes    int
	DeadlineAt time.Time
	Status     Status
	// Expiries counts the times the clock ran out unreported.
	Expiries int
	// CommitmentID is the open loop the order finishes.
	CommitmentID *uuid.UUID
	CreatedAt    time.Time
	ResolvedAt   *time.Time
}

// Overdue reports an open task whose clock has run out.
func (t Task) Overdue(now time.Time) bool {
	return t.Status == StatusOpen && !t.DeadlineAt.After(now)
}

// ClampMinutes keeps a clock inside what the board accepts.
func ClampMinutes(m int) int {
	return max(MinMinutes, min(MaxMinutes, m))
}

// RearmWindow is the clock a task gets after missing its deadline for the
// expiries-th time: the original allotment halved per miss, never under five
// minutes. Late means less time, not more.
func RearmWindow(minutes, expiries int) time.Duration {
	window := time.Duration(minutes) * time.Minute
	for range expiries {
		window /= 2
	}
	return max(minRearm, window)
}
