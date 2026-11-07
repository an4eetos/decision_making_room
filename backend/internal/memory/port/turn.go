package port

import "github.com/google/uuid"

// Turn is one finished exchange: what you said and what came back.
type Turn struct {
	SessionID     uuid.UUID
	MessageID     uuid.UUID
	UserText      string
	AssistantText string
	ModeID        string
}

// TurnObserver is told about each completed chat turn. It exists so other
// modules — commitments, today — can react to conversation without the memory
// module importing them. Observers must return immediately; anything slow
// belongs in a goroutine the observer owns, because the user is waiting on the
// response this is called from.
type TurnObserver interface {
	ObserveTurn(turn Turn)
}
