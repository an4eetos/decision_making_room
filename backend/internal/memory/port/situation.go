package port

import "context"

// SituationReader is the campaign map as the staff reads it, in words: the
// fronts, what sits on them, what moved, and what the numbers say. It is
// implemented outside the memory module and is optional. Without it, or on an
// empty map, the room answers exactly as it did before there was a map.
type SituationReader interface {
	// Situation returns the brief for a prompt, or "" when there is no map.
	Situation(ctx context.Context) (string, error)
}
