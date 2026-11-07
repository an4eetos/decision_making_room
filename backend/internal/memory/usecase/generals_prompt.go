package usecase

import (
	"fmt"
	"strings"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
)

// generalsPrompt builds the instruction block for the selected lenses when one
// call argues all of them. At depth the council runs each general as its own
// agent instead (council.go); everywhere else one call is the right trade —
// N calls multiply the latency, and "where they disagree" then needs one more
// call that re-reads all of them, which is exactly what the council pays for.
// lensJudgement keeps a lens from becoming a caricature. A lens told only to
// argue its doctrine will follow it off a cliff: recommend the all-out attack
// to someone with no reserves, or the long retreat to someone whose deadline is
// Friday, and then explain the loss away to stay in character. The lens is how
// a general looks at a situation, not a verdict reached before looking.
//
// seatJudgement is the same rule said to a council seat, which is the one
// deciding. Change them together.
const lensJudgement = `Each lens starts from its doctrine and usually lands there, but weighs it against reality first: ` +
	`what its move would cost this person here (time, money, health, people, the facts in their notes) against what the situation actually offers. ` +
	`Where its doctrine would bring an obvious loss, it says so and adjusts: a smaller move, other timing, or "my usual play is wrong here, because...". ` +
	`It never explains the loss away or doubles down to stay in character. The lens fixes how it looks, not what it concludes.`

const seatJudgement = `Start from your doctrine; you will usually land there, but weigh it against reality first: ` +
	`what your move would cost this person here (time, money, health, people, the facts in their notes) against what the situation actually offers. ` +
	`Where your doctrine would bring an obvious loss, say so and adjust: a smaller move, other timing, or "my usual play is wrong here, because...". ` +
	`Never explain the loss away or double down to stay in character. The lens fixes how you look, not what you conclude.`

// forecastItems ask each lens what its move brings beyond the result it is
// for. Advice that names only the intended outcome, plus the risks anyone would
// list, has done none of the work: the side effects and the slow ways a move
// goes wrong are what the person cannot see from where they stand. Both numbers
// are ceilings, not quotas: a lens with two real dangers names two, and a list
// padded to the number buries the ones that matter. Generic risks are banned
// outright for the same reason.
const forecastItems = `- Unexpected wins: up to three side effects in the person's favour that they are not counting on. Fewer real ones beat a stretched third.
- Where it goes worse: up to five or six mistakes or worse positions this move can land them in that are not obvious: second-order effects, slow drifts, what the move makes harder later, a failure that looks like success while it happens. Each with the early sign that it is starting. Name only the real ones; do not pad to the number.
Start from the card's "Typical side wins" and "Typically goes worse": check each against this situation, keep the ones that hold here, and add what is specific to this person. A typical one that does not apply here is left out.
Each item is specific to this person and this move, from their notes where possible. Risks anyone would name ("it might not work", "you could burn out", "it takes time") do not belong on either list.`

// forecastRules introduces forecastItems for whoever is making the move: "each
// lens", "the lens", or "you" in a council seat.
func forecastRules(who string) string {
	return "With the move, " + who + " what doing it actually brings, beyond the result it is for:\n" + forecastItems
}

// debateRules are what make the exchange worth having. Without them models
// answer a strawman, concede nothing, and converge on a polite average — three
// voices saying what one would have said.
const debateRules = `Rules for the exchange:
- Each lens replies to the strongest thing another lens said. Answering a weak version is worthless.
- A concession states what the other side gets right, with no "but" — the rebuttal already happened under Answers. It has to change something at the margin; "both have a point" is not a concession.
- Use each lens's "always asks" questions against the actual situation, and its "concedes when" honestly.
- Each lens owns the cost of its own advice. If a position walks past an obvious loss, the lens answering it names that loss.
- A lens may turn another's "where it goes worse" against it; that is often the strongest point to answer.
- Do not soften disagreement into consensus. If two lenses would give opposite advice, say so plainly.
- Frames, not characters: no period voice, no invented biography or quotes.`

// doctrine holds, by lens id, the passages chosen for this question; nil is
// cards only. structured reports whether the mode already imposes an output
// template. When it does, the lenses must not add their own headings on top —
// two competing structures get concatenated and the answer grows a second
// scaffold.
//
// forecast adds each lens's forecast of its own move; see forecastRules.
func generalsPrompt(lenses []gendomain.Lens, doctrine map[string][]gendomain.Passage, structured, forecast bool) string {
	if len(lenses) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString("You are answering through these planning lenses.\n")
	b.WriteString("Each has a job it is good at and a blind spot it is bad at. ")
	b.WriteString("Use them as frames for the advice, not as characters to perform — ")
	b.WriteString("do not write in period voice or invent biography.\n")
	// Frames, not characters, used to flatten every lens into the same polite
	// advisor. The register is part of the frame: a lens built on pressure that
	// arrives softened has lost the thing it was for.
	b.WriteString("Each lens keeps the register of its \"Sounds like\" line in its own part of the answer. " +
		"A blunt lens stays blunt and speaks to the person directly; do not soften it into neutral advice.\n\n")
	b.WriteString(lensJudgement)
	b.WriteString("\n\n")

	if len(doctrine) > 0 {
		// Without this the model argues from what it remembers about the
		// historical figure, which is exactly the generic version the doctrine
		// was written to replace.
		b.WriteString("Some lenses carry passages from their own doctrine, chosen for this question. ")
		b.WriteString("Argue that lens from its passage: it is the lens's actual position on this kind of " +
			"situation, and it outranks anything you know about the historical figure.\n\n")
	}

	b.WriteString(lensBlocks(lenses, doctrine))
	b.WriteString("\n\n")

	b.WriteString(exchangeShape(lenses, structured, forecast))
	return b.String()
}

// exchangeShape is how the answer is laid out once the lenses are in front of
// the model, whether it argues them itself or writes up a council's positions.
func exchangeShape(lenses []gendomain.Lens, structured, forecast bool) string {
	var b strings.Builder

	if len(lenses) == 1 {
		fmt.Fprintf(&b, "Answer in %s's frame throughout. Where that frame's blind spot "+
			"applies to this question, say so in one line rather than hiding it.", lenses[0].Name)
		if forecast {
			b.WriteString("\n\n" + forecastRules("forecast") + "\n")
			b.WriteString("Put them after the advice as **Unexpected wins:** and **Where it goes worse:**, one line each.")
		}
		return b.String()
	}

	if structured {
		// The mode owns the headings. The lenses argue inside them.
		b.WriteString("Keep the section structure given above — do not add sections for " +
			"each lens. Use them as competing perspectives within that structure: where they " +
			"would advise differently, put the disagreement inside the section it bears on, " +
			"let each lens answer the other's strongest point, name which you side with, and " +
			"say what siding with it costs.\n\n")
		if forecast {
			b.WriteString(forecastRules("each lens forecasts") + "\n")
			b.WriteString("Inside that structure, give each lens's forecast under its move, attributed " +
				"(**Name — unexpected wins:** / **Name — where it goes worse:**), one line per item.\n\n")
		}
		b.WriteString(debateRules)
		return b.String()
	}

	b.WriteString("Run it as an exchange, not a set of opinions. Structure the answer exactly like this:\n\n")
	for _, lens := range lenses {
		fmt.Fprintf(&b, "## %s — <their opening position, one line>\n", lens.Name)
		b.WriteString("Two to four sentences: the position, concretely, about this question.\n")
		if forecast {
			b.WriteString("**Unexpected wins:** up to three, one line each.\n")
			b.WriteString("**Where it goes worse:** up to five or six, one line each, with the early sign.\n")
		}
		b.WriteString("**Answers <the lens that disagrees most>:** meet their strongest point, not a weaker version of it.\n")
		b.WriteString("**Concedes:** the one thing the other side has right, and what it changes.\n\n")
	}
	b.WriteString("## The fork\n")
	b.WriteString("The real disagreement, as one question the person has to answer. If the lenses " +
		"genuinely agree, write \"No real fork — they all say X\" and do not invent one.\n\n")
	b.WriteString("## The call\n")
	b.WriteString("Which lens you side with, what you take from the others, and what siding costs.\n\n")
	if forecast {
		b.WriteString(forecastRules("each lens forecasts") + "\n\n")
	}
	b.WriteString(debateRules)

	return b.String()
}

// lensBlocks renders each card with its doctrine passages directly beneath it,
// so a passage is never read as belonging to the wrong lens.
func lensBlocks(lenses []gendomain.Lens, doctrine map[string][]gendomain.Passage) string {
	parts := make([]string, 0, len(lenses))
	for _, lens := range lenses {
		var b strings.Builder
		b.WriteString(lens.Card())
		for _, p := range doctrine[lens.ID] {
			section := p.Section
			if section == "" {
				section = "doctrine"
			}
			fmt.Fprintf(&b, "\nFrom %s's doctrine — %s:\n%s", lens.Name, section, p.Text)
		}
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "\n\n")
}
