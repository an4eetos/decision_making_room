package domain

import (
	"time"

	"github.com/google/uuid"
)

// Appraisal is the staff's reading of the map, written on request: the
// point of main effort, where to yield ground, what is overextended. It is a
// judgment over the sitrep, not a view of the data, which is why it is kept
// with its date rather than recomputed.
type Appraisal struct {
	ID uuid.UUID
	// FrontID is the front it appraised; nil is the whole map.
	FrontID *uuid.UUID
	// Tier is the depth that actually ran, and Generals the lenses it was
	// written through.
	Tier     string
	Generals []string
	Body     string
	// SessionID is set once you discuss it; the conversation is created then,
	// not before.
	SessionID *uuid.UUID
	CreatedAt time.Time
}
