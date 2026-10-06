// Package domain models conversation modes: what kind of question this is, and
// therefore how to retrieve for it and how to shape the answer.
package domain

// Family groups modes by what you are trying to do. Detection uses it for
// stickiness — drifting within a family is fine, jumping between them is not.
type Family string

const (
	FamilyPlan    Family = "plan"
	FamilyDecide  Family = "decide"
	FamilyUnblock Family = "unblock"
	FamilyReview  Family = "review"
	FamilyOpen    Family = "open"
	// FamilyInterrogate questions instead of advising. Its modes are entered on
	// purpose, never detected.
	FamilyInterrogate Family = "interrogate"
)

// Mode is one conversation shape.
//
// These ship as markdown rather than Go structs or database rows. Structs would
// mean recompiling to change a prompt, which is disqualifying for a tool whose
// value is your own thinking. Rows would mean that improving a shipped prompt
// needs a migration that has to guess whether you had edited it.
type Mode struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Family  Family   `yaml:"family"`
	Summary string   `yaml:"summary"`
	Aliases []string `yaml:"aliases"`

	Triggers  Triggers  `yaml:"triggers"`
	Retrieval Retrieval `yaml:"retrieval"`
	Generals  Generals  `yaml:"generals"`
	// Styles are working styles this mode leans on. They describe execution, so
	// they inform the answer's tone rather than being argued between.
	Styles []string `yaml:"styles"`

	// ExplicitOnly keeps a mode out of detection entirely: it runs only when
	// picked. Interrogation is one — being questioned when you asked for advice
	// is worse than any misdetected template.
	ExplicitOnly bool `yaml:"explicit_only"`

	// SystemPrompt and OutputPrompt come from the "## System" and "## Output"
	// sections of the body. ConcludePrompt comes from "## Position" and replaces
	// the output template on the turn that closes the mode.
	SystemPrompt   string `yaml:"-"`
	OutputPrompt   string `yaml:"-"`
	ConcludePrompt string `yaml:"-"`
	Source         string `yaml:"-"`
}

type Triggers struct {
	Keywords []string `yaml:"keywords"`
	// Weight scales this mode's keyword hits. A mode with unavoidably generic
	// triggers can be damped below one so it does not win on common words.
	Weight float64 `yaml:"weight"`
}

// Retrieval biases the search for this kind of question. Kinds are boosted and
// never filtered: on a single-user corpus of a few thousand rows, excluding a
// kind outright throws away good hits for no gain.
type Retrieval struct {
	Kinds     []string `yaml:"kinds"`
	KindBoost float64  `yaml:"kind_boost"`
	// WindowDays is advisory, expressed through recency weighting rather than a
	// hard cutoff — a decision from last year is still the answer to a question
	// about that decision.
	WindowDays int `yaml:"window_days"`
	TopK       int `yaml:"top_k"`
	// RecencyWeight overrides the global default. A debrief wants recency to
	// dominate; a pre-mortem wants it almost ignored.
	RecencyWeight float64 `yaml:"recency_weight"`
}

type Generals struct {
	Default []string `yaml:"default"`
	Max     int      `yaml:"max"`
	// Doctrine names the doctrine sections this mode leans on — a pre-mortem
	// wants "The case against", not "Over a long game". Boosted, never filtered:
	// a passage that matches the question still beats one that matches the mode.
	Doctrine []string `yaml:"doctrine"`
	// KeepSeated keeps the conversation's generals when switching into this
	// mode. Normally a mode switch earns a fresh pick; an interrogation should
	// be run by the generals who saw the trap in the first place.
	KeepSeated bool `yaml:"keep_seated"`
}

// IsOpen reports the unstructured default, which applies no output template.
func (m Mode) IsOpen() bool { return m.Family == FamilyOpen }

// IsInterrogation reports a mode that questions instead of advising.
func (m Mode) IsInterrogation() bool { return m.Family == FamilyInterrogate }

type Registry struct {
	Modes []Mode
}
