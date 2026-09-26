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
// debateRules are what make the exchange worth having. Without them models
// answer a strawman, concede nothing, and converge on a polite average — three
// voices saying what one would have said.
const debateRules = `Rules for the exchange:
- Each lens replies to the strongest thing another lens said. Answering a weak version is worthless.
- A concession states what the other side gets right, with no "but" — the rebuttal already happened under Answers. It has to change something at the margin; "both have a point" is not a concession.
- Use each lens's "always asks" questions against the actual situation, and its "concedes when" honestly.
- Do not soften disagreement into consensus. If two lenses would give opposite advice, say so plainly.
- Frames, not characters: no period voice, no invented biography or quotes.`

// structured reports whether the mode already imposes an output template. When
// it does, the lenses must not add their own headings on top — two competing
// structures get concatenated and the answer grows a second scaffold.
func generalsPrompt(lenses []gendomain.Lens, structured bool) string {
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

	if structured {
		// The mode owns the headings. The lenses argue inside them.
		b.WriteString("Keep the section structure given above — do not add sections for " +
			"each lens. Use them as competing perspectives within that structure: where they " +
			"would advise differently, put the disagreement inside the section it bears on, " +
			"let each lens answer the other's strongest point, name which you side with, and " +
			"say what siding with it costs.\n\n")
		b.WriteString(debateRules)
		return b.String()
	}

	b.WriteString("Run it as an exchange, not a set of opinions. Structure the answer exactly like this:\n\n")
	for _, lens := range lenses {
		fmt.Fprintf(&b, "## %s — <their opening position, one line>\n", lens.Name)
		b.WriteString("Two to four sentences: the position, concretely, about this question.\n")
		b.WriteString("**Answers <the lens that disagrees most>:** meet their strongest point, not a weaker version of it.\n")
		b.WriteString("**Concedes:** the one thing the other side has right, and what it changes.\n\n")
	}
	b.WriteString("## The fork\n")
	b.WriteString("The real disagreement, as one question the person has to answer. If the lenses " +
		"genuinely agree, write \"No real fork — they all say X\" and do not invent one.\n\n")
	b.WriteString("## The call\n")
	b.WriteString("Which lens you side with, what you take from the others, and what siding costs.\n\n")
	b.WriteString(debateRules)

	return b.String()
}
