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

// Kind is what sort of order a commitment is. Most are plain orders; a recon
// order is aimed at an unknown and exists to lift fog — finding something out
// rather than getting something done.
type Kind string

const (
	KindOrder Kind = "order"
	KindRecon Kind = "recon"
)

func (k Kind) Valid() bool { return k == KindOrder || k == KindRecon }

type Commitment struct {
	ID   uuid.UUID
	Text string
	// Kind is order or recon; empty is read as order.
	Kind Kind
	// TargetID is the campaign item the order is aimed at: the objective it
	// serves, the obstacle it attacks, or the unknown a recon order scouts. Nil
	// is an order aimed at nothing on the map, which is most of them.
	TargetID    *uuid.UUID
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
