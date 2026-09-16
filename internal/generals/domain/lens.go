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
// Only the card reaches the prompt — see Card. The full Doctrine is retrievable
// but never injected, because injecting every general's full text is what made
// the original cost ~5,000 tokens on every single question regardless of what
// was being asked.
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

	// Doctrine is the full body text. Retrievable, never injected by default.
	Doctrine string `yaml:"-"`
	// Source is "builtin" or the path it was overlaid from.
	Source string `yaml:"-"`
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
}
