// Package domain models the planning lenses: generals, who argue about strategy,
// and working styles, which describe how you actually execute.
package domain

import (
	"fmt"
	"strings"
)

// Kind separates the two rosters. They share a shape because they answer the
// same four questions — what is this for, when does it fit, when does it not,
// and what does it sound like — but they are picked at different moments.
type Kind string

const (
	// KindGeneral is a strategic lens. You pick up to three and they argue.
	KindGeneral Kind = "general"
	// KindStyle is an execution posture. Modes reference these; you do not pick
	// them by hand.
	KindStyle Kind = "style"
)

// Family groups lenses by what they are good for. Selection uses it to break
// ties toward a lens that suits the kind of question being asked.
type Family string

const (
	FamilyContact       Family = "contact"       // start now, make contact, fix later
	FamilyScouting      Family = "scouting"      // understand the ground before spending
	FamilyEndurance     Family = "endurance"     // show up again; win by not stopping
	FamilyAdaptation    Family = "adaptation"    // the plan met reality; change it
	FamilySystems       Family = "systems"       // templates, delegation, staff work
	FamilyConcentration Family = "concentration" // all force at one point
	FamilyPreservation  Family = "preservation"  // decline the battle; keep what you have
)

// Lens is one general or working style.
//
// The card always reaches the prompt — see Card. The Doctrine never reaches it
// whole, because injecting every general's full text is what made the original
// cost ~5,000 tokens on every single question regardless of what was being
// asked. Instead it is split into Passages, and the one or two that bear on the
// question ride along under the card.
type Lens struct {
	ID      string `yaml:"id"`
	Kind    Kind   `yaml:"-"`
	Name    string `yaml:"name"`
	Epithet string `yaml:"epithet"`
	Era     string `yaml:"era"`
	Family  Family `yaml:"family"`

	Job        string   `yaml:"job"`
	DeployWhen []string `yaml:"deploy_when"`
	AvoidWhen  []string `yaml:"avoid_when"`
	SoundsLike string   `yaml:"sounds_like"`

	// Bias is what this lens systematically gets wrong. It is in the card on
	// purpose: without it a multi-general answer collapses into three voices
	// agreeing, which is worth nothing over one voice.
	Bias string `yaml:"bias"`

	Routes Routes `yaml:"routes"`

	// Asks are the questions this lens always puts to a situation. They are
	// what make a lens argue from its own ground instead of agreeing politely.
	Asks []string `yaml:"asks"`
	// Unknowns is the lens's stance on uncertainty in one line: how it sorts
	// what is not known and what it does about it. Lenses disagree about the
	// unknown at least as much as about the known, and the card is the only
	// place that disagreement can reach the prompt.
	Unknowns string `yaml:"unknowns"`
	// ConcedesWhen names the conditions under which this lens yields. A lens
	// that can never be wrong cannot take part in a real exchange.
	ConcedesWhen []string `yaml:"concedes_when"`
	// Rivals are ids of lenses it most naturally argues against. Selection
	// uses them to seat opponents together, so an answer has a real fork in it.
	Rivals []string `yaml:"rivals"`
	// RivalNames is Rivals resolved to display names at load time.
	RivalNames []string `yaml:"-"`

	// Kills are ids of the traps this lens refuses to let stand — the cognitive
	// mistakes it is built to catch. They decide who signs a suggestion to be
	// interrogated and which traps an interrogation carries. They stay out of
	// the card, which is at its budget: an ordinary answer gets the whole trap
	// list through the flag instruction instead.
	Kills []string `yaml:"kills"`
	// KillNames is Kills resolved to display names at load time.
	KillNames []string `yaml:"-"`
	// Orders are the lens's direct orders, one line each, written so they can be
	// carried out on an ordinary problem. They reach only interrogation and the
	// position that closes it, never an ordinary card.
	Orders []string `yaml:"orders"`

	// Portrait is optional artwork, credited.
	Portrait Portrait `yaml:"portrait"`

	// Doctrine is the full body text. Reaches a prompt only as Passages chosen
	// for the question, or whole through the read_doctrine tool on deep.
	Doctrine string `yaml:"-"`
	// Source is "builtin" or the path it was overlaid from.
	Source string `yaml:"-"`
}

// Portrait is a picture of the lens, with the credit its licence requires.
type Portrait struct {
	File    string `yaml:"file" json:"file,omitempty"`
	Credit  string `yaml:"credit" json:"credit,omitempty"`
	License string `yaml:"license" json:"license,omitempty"`
	Source  string `yaml:"source" json:"source,omitempty"`
}

// Routes drive deterministic selection — no model call, no latency.
type Routes struct {
	Keywords []string `yaml:"keywords"`
	// Modes lists mode ids this lens suits. Populated once modes exist.
	Modes []string `yaml:"modes"`
}

// Card renders the compact form that goes into a prompt: roughly 90 to 120
// tokens. Three of these cost about 350 tokens against the ~5,000 the original
// spent injecting all nine generals in full, every turn, ranked or not.
func (l Lens) Card() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s", l.Name)
	if l.Epithet != "" {
		fmt.Fprintf(&b, " — %s", l.Epithet)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "Job: %s\n", l.Job)

	if len(l.DeployWhen) > 0 {
		fmt.Fprintf(&b, "Fits when: %s\n", strings.Join(l.DeployWhen, "; "))
	}
	if len(l.AvoidWhen) > 0 {
		fmt.Fprintf(&b, "Wrong when: %s\n", strings.Join(l.AvoidWhen, "; "))
	}
	if l.SoundsLike != "" {
		fmt.Fprintf(&b, "Sounds like: %q\n", l.SoundsLike)
	}
	if l.Bias != "" {
		fmt.Fprintf(&b, "Blind spot: %s\n", l.Bias)
	}
	if len(l.Asks) > 0 {
		fmt.Fprintf(&b, "Always asks: %s\n", strings.Join(l.Asks, " / "))
	}
	if l.Unknowns != "" {
		fmt.Fprintf(&b, "Facing the unknown: %s\n", l.Unknowns)
	}
	if len(l.ConcedesWhen) > 0 {
		fmt.Fprintf(&b, "Concedes when: %s\n", strings.Join(l.ConcedesWhen, "; "))
	}

	if len(l.RivalNames) > 0 {
		fmt.Fprintf(&b, "Argues most with: %s\n", strings.Join(l.RivalNames, ", "))
	}

	return strings.TrimRight(b.String(), "\n")
}

// Cards renders several lenses as one block.
func Cards(lenses []Lens) string {
	parts := make([]string, 0, len(lenses))
	for _, l := range lenses {
		parts = append(parts, l.Card())
	}
	return strings.Join(parts, "\n\n")
}

// Roster is the loaded, validated set.
type Roster struct {
	Generals []Lens
	Styles   []Lens
	Traps    []Trap
}
