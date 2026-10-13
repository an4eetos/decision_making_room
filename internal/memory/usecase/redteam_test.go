package usecase

import (
	"strings"
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

// A red team is one general commanding the opposition: no exchange, no fork,
// no flag, and the map's positions handed over as its order of battle.
func TestRedTeamPromptHandsTheOppositionToOneGeneral(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	plan := ConsultPlan{
		Mode:      testMode(t, "red_team"),
		Generals:  reg.Resolve([]string{"manstein"}),
		Traps:     reg.Traps(),
		Tier:      domain.PolicyFor(domain.TierStandard),
		Situation: "Front: Work — line 20%\n  ◆ Ship v2 — active\n    ▲ No tests — too big to hold, fortress (est)",
	}
	prompt := buildSystemPrompt(systemPrompt, plan)

	for _, want := range []string{
		"This is a red team", "Manstein takes command of the opposition",
		"**Enemy commander**", "**Where the plan breaks**", "**Your fog, my cover**", "**I concede**", "**To beat me**",
		"The positions marked ▲ there are yours", "Rules of the red team", "Manstein — The Operator",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("red team prompt missing %q", want)
		}
	}
	for _, unwanted := range []string{"## The fork", "## The call", "[[interrogate:", "Unexpected wins", "There is no map"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("red team prompt should not contain %q", unwanted)
		}
	}
	if plan.UsesCouncil() {
		t.Error("a red team never convenes a council")
	}
	if suggest(plan, []string{"spotlight"}) != nil {
		t.Error("the staff reads the map, not the person's words: no interrogation suggested")
	}

	// Without a map the commander works from the person's words, and says so.
	plan.Situation = ""
	prompt = buildSystemPrompt(systemPrompt, plan)
	if !strings.Contains(prompt, "There is no map") {
		t.Error("with no map the red team should say it works from the person's words alone")
	}
}

// A staff appraisal is the ordinary exchange laid over the mode's sections:
// the lenses argue inside them and never add their own scaffold.
func TestStaffAppraisalArguesInsideTheTemplate(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	plan := ConsultPlan{
		Mode:     testMode(t, "staff"),
		Generals: reg.Resolve([]string{"manstein", "kutuzov"}),
		Traps:    reg.Traps(),
		Tier:     domain.PolicyFor(domain.TierDeep),
	}
	prompt := buildSystemPrompt(systemPrompt, plan)

	for _, want := range []string{
		"This is a staff appraisal", "**Point of main effort**", "**Yield ground**", "**Overextended**",
		"Keep the section structure given above", "Kutuzov — The Patient One",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("staff prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "## The fork") {
		t.Error("the mode owns the headings; no second scaffold")
	}
	if !plan.UsesCouncil() {
		t.Error("at depth the staff convenes as a council")
	}
}
