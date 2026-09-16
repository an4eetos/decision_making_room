package usecase

import (
	"fmt"
	"strings"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
)

// generalsPrompt builds the instruction block for the selected lenses.
//
// One model call, not one per general. N calls triple the latency, the per-general
// answers cannot see each other, and "where they disagree" would then need a
// fourth call that re-reads all of them.
func generalsPrompt(lenses []gendomain.Lens) string {
	if len(lenses) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("You are answering through these planning lenses.\n")
	b.WriteString("Each has a job it is good at and a blind spot it is bad at. ")
	b.WriteString("Use them as frames for the advice, not as characters to perform — ")
	b.WriteString("do not write in period voice or invent biography.\n\n")

	b.WriteString(gendomain.Cards(lenses))
	b.WriteString("\n\n")

	if len(lenses) == 1 {
		fmt.Fprintf(&b, "Answer in %s's frame throughout. Where that frame's blind spot "+
			"applies to this question, say so in one line rather than hiding it.", lenses[0].Name)
		return b.String()
	}

	b.WriteString("Structure the answer exactly like this:\n\n")
	for _, lens := range lenses {
		fmt.Fprintf(&b, "## %s — <their one-line position>\n", lens.Name)
		b.WriteString("Two to four sentences. Concrete, about this question, in this lens's frame.\n\n")
	}
	b.WriteString("## Where they disagree\n")
	b.WriteString("One line naming the actual fork. If they genuinely agree, write ")
	b.WriteString("\"No real disagreement — both say X.\"\n\n")
	b.WriteString("## The call\n")
	b.WriteString("Your synthesis. Name which lens you are siding with and what that costs.\n\n")

	// The single most load-bearing line. Without it three lenses converge into
	// polite agreement and the whole feature is worth nothing over one voice.
	b.WriteString("Do not soften disagreement into consensus. If two lenses would give " +
		"opposite advice, say so plainly and name the fork.")

	return b.String()
}
