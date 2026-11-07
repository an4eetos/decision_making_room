package domain

import (
	"fmt"
	"strings"
)

// Trap is a cognitive mistake the room watches for: a way of reading the
// situation that feels like information and is not. Public shame nobody will
// remember, pain forecast to last for months that lasts a week, "no choice".
//
// Traps are data, like the lenses, so the catalogue can be rewritten through
// the overlay without recompiling. Each general names the traps it kills, which
// is what decides who signs a suggestion to be interrogated and which traps an
// interrogation prompt carries.
type Trap struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
	// Tell is how the trap shows up in what someone says or assumes.
	Tell string `yaml:"tell"`
	// Signals are phrases that suggest the trap is active. Multi-word phrases
	// match as substrings, single words as whole words.
	Signals []string `yaml:"signals"`
	// Kill is the question that exposes it. Answered honestly, the trap has
	// nothing left to stand on.
	Kill string `yaml:"kill"`
	// Weak traps are too common in ordinary speech to justify interrupting
	// someone on their own. They count toward a suggestion only alongside a
	// trap that is not weak.
	Weak bool `yaml:"weak"`
}

// Line renders the trap for a prompt in one line.
func (t Trap) Line() string {
	return fmt.Sprintf("%s (%s) — %s Kill it with: %s", t.Name, t.ID, t.Tell, t.Kill)
}

// TrapLines renders several traps as a list.
func TrapLines(traps []Trap) string {
	lines := make([]string, 0, len(traps))
	for _, t := range traps {
		lines = append(lines, "- "+t.Line())
	}
	return strings.Join(lines, "\n")
}
