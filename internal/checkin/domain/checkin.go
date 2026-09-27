// Package domain models check-ins: the room asking how things are going,
// instead of waiting to be asked.
package domain

import (
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	// KindScheduled fires at a configured time of day.
	KindScheduled Kind = "scheduled"
	// KindNudge fires on a condition: you have gone quiet, or something you
	// committed to has gone stale.
	KindNudge Kind = "nudge"
)

type CheckIn struct {
	ID   uuid.UUID
	Slot string
	Kind Kind
	// LocalDate is the calendar day it belongs to, in the configured timezone.
	// With Slot it makes firing idempotent across restarts and laptop sleep.
	LocalDate time.Time
	// ModeID is the mode the conversation opens in when you reply.
	ModeID string
	Title  string
	Body   string
	// SessionID is set once you open the check-in. Conversations are created
	// lazily so that check-ins you never answer do not fill the chat list.
	SessionID *uuid.UUID
	SeenAt    *time.Time
	CreatedAt time.Time
}
