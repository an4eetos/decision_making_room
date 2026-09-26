// Package domain models open loops: things you said you would do.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	// StatusProposed is extracted from conversation and waiting for you to keep
	// or drop it. Nothing becomes open without your agreement: an auto-generated
	// todo list you never agreed to is how this kind of feature dies.
	StatusProposed Status = "proposed"
	StatusOpen     Status = "open"
	StatusDone     Status = "done"
	StatusDropped  Status = "dropped"
	// StatusStale is open and untouched for long enough that reminding you has
	// become nagging. Stale rows leave check-in prompts but stay visible.
	StatusStale Status = "stale"
)

// Live reports whether the commitment still counts as outstanding.
func (s Status) Live() bool { return s == StatusProposed || s == StatusOpen }

// Resolved reports whether it has left the list for good.
func (s Status) Resolved() bool { return s == StatusDone || s == StatusDropped }

func (s Status) Valid() bool {
	switch s {
	case StatusProposed, StatusOpen, StatusDone, StatusDropped, StatusStale:
		return true
	default:
		return false
	}
}

type Source string

const (
	SourceChat    Source = "chat"
	SourceManual  Source = "manual"
	SourceCheckin Source = "checkin"
)

type Commitment struct {
	ID          uuid.UUID
	Text        string
	Status      Status
	DueAt       *time.Time
	Source      Source
	SessionID   *uuid.UUID
	MessageID   *uuid.UUID
	ModeID      string
	Confidence  float64
	Fingerprint string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ResolvedAt  *time.Time
}

// Age is how long since it was last touched, which is what staleness measures.
func (c Commitment) Age(now time.Time) time.Duration {
	return now.Sub(c.UpdatedAt)
}
