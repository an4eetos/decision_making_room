package domain

// Suggestion is the room recommending an interrogation, attached to the answer
// that prompted it. It is stored with the message, so the banner survives a
// reload and the cooldown can see when the last one was made.
type Suggestion struct {
	Traps []TrapFound `json:"traps"`
	// By is the general who signs it: the seated one who kills the first trap.
	By string `json:"by"`
	// Source is "signal" when phrases found it, "model" when only the model saw
	// it, and "both".
	Source string `json:"source"`
}

// TrapFound is one trap behind a suggestion. Quote is the person's own
// sentence when a phrase found it; a trap only the model saw has none.
type TrapFound struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Quote  string `json:"quote,omitempty"`
	Source string `json:"source"`
}

const (
	SuggestionSignal = "signal"
	SuggestionModel  = "model"
	SuggestionBoth   = "both"
)
