package usecase

import (
	"fmt"
	"strings"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
)

// redTeamRules keep the attack honest and usable. An attack the person
// cannot find on their map, or that could not happen to them, is noise; a red
// team that concedes nothing has found nothing; and an attack that ends
// without saying what beats it has only frightened someone.
const redTeamRules = `Rules of the red team:
- Attack the plan, never the person. Every attack names the objective, order or position it hits, and the early sign they will see when it is working.
- Stay inside reality: use their notes and the dates and strengths on the map. An attack that could not happen to this person, in this situation, is noise.
- The kinds of stuck and the strengths on the map are your order of battle. Hold each position the way its kind holds: waiting holds by their not chasing it, fear by their not looking, too big by never being cut down, not really wanted because they do not want to win.
- Turn the lens's own doctrine against them: its "Always asks" are the questions that find the gap, its "Typically goes worse" is your playbook, and its "Concedes when" is what you refuse to give them.
- Concede plainly where the plan is strong, once. Then say what it would take to beat you, as orders they could give.
- Frames, not characters: no period voice, no invented biography or quotes.`

// redTeamPrompt replaces the generals' exchange when one general takes
// command of the opposition. The exchange is advice, and advice is the one
// thing a red team withholds: it attacks, concedes what holds, and says what
// would beat it. One general, because two enemies argue with each other
// instead of with the plan.
//
// hasMap says whether the campaign map reached the context; without it the
// commander works from the person's words alone and says so.
func redTeamPrompt(lenses []gendomain.Lens, doctrine map[string][]gendomain.Passage, hasMap bool) string {
	if len(lenses) == 0 {
		return ""
	}
	commander := lenses[0]

	var b strings.Builder
	fmt.Fprintf(&b, "%s takes command of the opposition. ", commander.Name)
	b.WriteString("The lens is the enemy's doctrine now: how this general would hold the positions marked ▲ on the person's map, " +
		"use the fog marked ? as cover, and answer each order they have given. ")
	b.WriteString("Speak as the commander of the opposition, to the person, in the second person: \"your plan\", \"my positions\". ")
	b.WriteString("Keep the register of the \"Sounds like\" line; a blunt lens attacks bluntly.\n\n")

	if hasMap {
		b.WriteString("The map is under \"The campaign map\" in the context. The positions marked ▲ there are yours, the fog ? is your cover, and the objectives ◆ are what you hold against.\n\n")
	} else {
		b.WriteString("There is no map. Take the plan, the objective and what opposes it from what they wrote, and say once that you are working from their words alone.\n\n")
	}

	// The lens still weighs reality: an enemy that holds where nothing could
	// hold has taught the person nothing.
	b.WriteString(lensJudgement)
	b.WriteString("\n\n")

	b.WriteString(lensBlocks([]gendomain.Lens{commander}, doctrine))
	if len(doctrine[commander.ID]) > 0 {
		b.WriteString("\n\nArgue from the doctrine passages under the card, turned to the opposition's side: they say how this lens holds ground and reads an advance, and they outrank anything you know about the historical figure.")
	}
	b.WriteString("\n\n")
	b.WriteString(redTeamRules)
	b.WriteString("\n\nKeep the section structure given above exactly; the section names are fixed and the room reads them.")
	return b.String()
}
