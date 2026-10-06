package usecase

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	"github.com/an4eetos/decision-room/internal/memory/domain"
)

// alwaysHunted are traps every interrogation carries whoever is seated: being
// frozen by pressure and answering in fog are what an interrogation exists to
// break, so they are never left to the luck of the roster.
var alwaysHunted = []string{"encircled", "vague_intent"}

// maxSuggestedTraps bounds a banner. More than three names is a diagnosis, not
// a reason to stop and be questioned.
const maxSuggestedTraps = 3

// interrogationRules are what keep the lenses questioning instead of sliding
// back into advice, which is where a model goes the moment the rules loosen.
const interrogationRules = `Rules for the interrogation:
- Each lens asks at most one question per turn, from its own "Always asks", its orders and its doctrine, in the register of its "Sounds like" line. No two lenses ask the same thing.
- Label every question with the lens's name in bold: **Name:** question.
- A question must be answerable with a fact, a number, a date or a name. "How do you feel about it?" is not a question here.
- Name a trap only when the person's own words show it, and quote those words. Never diagnose from a hunch, and never paste a trap's description: say what it assumes in their situation.
- Frames, not characters: no period voice, no invented biography or quotes.`

// interrogationPrompt replaces the generals' exchange in an interrogation. The
// exchange template — positions, the fork, the call — is advice, and advice is
// exactly what an interrogation withholds until the position is earned.
//
// spotted are trap ids found in what the person wrote; they are hunted whoever
// is seated, so a trap is never named after the nearest one a lens happens to
// carry.
func interrogationPrompt(lenses []gendomain.Lens, doctrine map[string][]gendomain.Passage, traps []gendomain.Trap, spotted []string, conclude bool) string {
	if len(lenses) == 0 {
		return ""
	}

	var b strings.Builder
	if conclude {
		b.WriteString("These lenses ran the interrogation. Now they give orders, not questions.\n")
	} else {
		b.WriteString("These lenses are interrogating. They do not advise yet; they question, " +
			"each from its own ground, until the person's position no longer rests on a trap.\n")
	}
	b.WriteString("Each lens keeps the register of its \"Sounds like\" line and speaks to the person directly. " +
		"Do not soften a blunt lens into a polite interviewer.\n\n")

	for i, lens := range lenses {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(lensBlocks([]gendomain.Lens{lens}, doctrine))
		if len(lens.KillNames) > 0 {
			fmt.Fprintf(&b, "\n%s hunts: %s", lens.Name, strings.Join(lens.KillNames, ", "))
		}
		if len(lens.Orders) > 0 {
			fmt.Fprintf(&b, "\n%s's orders:", lens.Name)
			for _, order := range lens.Orders {
				fmt.Fprintf(&b, "\n- %s", order)
			}
		}
	}

	if hunted := huntedTraps(lenses, traps, spotted); len(hunted) > 0 {
		b.WriteString("\n\nTraps these lenses hunt. Each comes with the question that kills it:\n")
		b.WriteString(gendomain.TrapLines(hunted))
	}

	b.WriteString("\n\n")
	if conclude {
		b.WriteString("Under Orders, attribute each order to the lens it comes from (**Name:** order), " +
			"adapted from that lens's orders to this exact situation: a direction, a time and a fallback, " +
			"not a principle.")
		return b.String()
	}
	b.WriteString(interrogationRules)
	return b.String()
}

// huntedTraps is the seated lenses' kills, the always-hunted ones and any the
// person's own words showed, in the catalogue's order so the prompt is stable
// from turn to turn.
func huntedTraps(lenses []gendomain.Lens, traps []gendomain.Trap, spotted []string) []gendomain.Trap {
	want := make(map[string]bool)
	for _, id := range alwaysHunted {
		want[id] = true
	}
	for _, id := range spotted {
		want[id] = true
	}
	for _, lens := range lenses {
		for _, id := range lens.Kills {
			want[id] = true
		}
	}

	var out []gendomain.Trap
	for _, trap := range traps {
		if want[trap.ID] {
			out = append(out, trap)
		}
	}
	return out
}

// flagPrompt asks the model to mark the traps phrases cannot see: the shame
// nobody named, the pain forecast between the lines. One line at the very end,
// in a fixed shape, so it can be cut out before anyone reads the answer.
func flagPrompt(traps []gendomain.Trap) string {
	if len(traps) == 0 {
		return ""
	}

	ids := make([]string, 0, len(traps))
	for _, trap := range traps {
		ids = append(ids, fmt.Sprintf("%s (%s)", trap.ID, trap.Name))
	}

	return "After the answer, check the person's message for cognitive traps. If one of these is clearly " +
		"at work in what they wrote or plainly assume, end with one final line, exactly " +
		"`[[interrogate: id, id]]`, using at most three of these ids: " + strings.Join(ids, ", ") + ". " +
		"If none clearly is, write no such line. Never mention the line or the traps in the answer itself."
}

// interrogateFlag matches the flag line anywhere it ended up: models sometimes
// put a blank line or a stray space around it, occasionally write it twice,
// and sometimes drop the "interrogate:" prefix and leave only the ids. The id
// list is restricted to id characters, so a bracketed phrase in prose is never
// mistaken for a flag.
var interrogateFlag = regexp.MustCompile(`(?mi)^[ \t]*\[\[(?:interrogate:)?([a-z_, \t]*)\]\][ \t]*$`)

// parseInterrogateFlag removes every flag line and returns the ids they named,
// lower-cased and de-duplicated. Unknown ids are left for suggest to drop,
// because only it holds the catalogue.
func parseInterrogateFlag(answer string) (string, []string) {
	matches := interrogateFlag.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return answer, nil
	}

	var ids []string
	for _, m := range matches {
		for _, id := range strings.Split(m[1], ",") {
			id = strings.ToLower(strings.TrimSpace(id))
			if id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}

	clean := interrogateFlag.ReplaceAllString(answer, "")
	return strings.TrimSpace(clean), ids
}

// suggest merges what the phrases found with what the model flagged into one
// recommendation, signed by the seated general who kills the first trap.
//
// A suggestion needs at least one trap that is not weak, whoever found it: a
// model flagging only "vague intent" is no more a reason to interrupt someone
// than the phrase "try to" is.
func suggest(plan ConsultPlan, flagged []string) *domain.Suggestion {
	// Inside an interrogation the hits feed the interrogators instead.
	if plan.Plain || plan.Mode.IsInterrogation() || len(plan.Traps) == 0 {
		return nil
	}

	byID := make(map[string]gendomain.Trap, len(plan.Traps))
	for _, trap := range plan.Traps {
		byID[trap.ID] = trap
	}

	var found []domain.TrapFound
	index := make(map[string]int)
	for _, hit := range plan.TrapHits {
		index[hit.ID] = len(found)
		found = append(found, domain.TrapFound{ID: hit.ID, Name: hit.Name, Quote: hit.Quote, Source: domain.SuggestionSignal})
	}
	for _, id := range flagged {
		trap, ok := byID[id]
		if !ok {
			continue
		}
		if i, seen := index[id]; seen {
			found[i].Source = domain.SuggestionBoth
			continue
		}
		index[id] = len(found)
		found = append(found, domain.TrapFound{ID: id, Name: trap.Name, Source: domain.SuggestionModel})
	}

	strong := slices.ContainsFunc(found, func(f domain.TrapFound) bool { return !byID[f.ID].Weak })
	if !strong {
		return nil
	}
	// Strong traps lead: they are the reason to stop, and the first one picks
	// who signs.
	slices.SortStableFunc(found, func(a, b domain.TrapFound) int {
		switch wa, wb := byID[a.ID].Weak, byID[b.ID].Weak; {
		case wa == wb:
			return 0
		case wb:
			return -1
		default:
			return 1
		}
	})
	if len(found) > maxSuggestedTraps {
		found = found[:maxSuggestedTraps]
	}

	return &domain.Suggestion{Traps: found, By: signer(plan.Generals, found), Source: overallSource(found)}
}

// signer is the seated general who kills the earliest trap, falling back to
// whoever is seated first. The person should hear it from the lens whose job
// this trap is.
func signer(lenses []gendomain.Lens, found []domain.TrapFound) string {
	for _, f := range found {
		for _, lens := range lenses {
			if slices.Contains(lens.Kills, f.ID) {
				return lens.ID
			}
		}
	}
	if len(lenses) > 0 {
		return lenses[0].ID
	}
	return ""
}

func overallSource(found []domain.TrapFound) string {
	signal, model := false, false
	for _, f := range found {
		switch f.Source {
		case domain.SuggestionSignal:
			signal = true
		case domain.SuggestionModel:
			model = true
		default:
			signal, model = true, true
		}
	}
	switch {
	case signal && model:
		return domain.SuggestionBoth
	case model:
		return domain.SuggestionModel
	default:
		return domain.SuggestionSignal
	}
}
