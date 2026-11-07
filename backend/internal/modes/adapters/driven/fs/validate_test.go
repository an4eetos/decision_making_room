package fs

import (
	"strings"
	"testing"

	"github.com/an4eetos/decision-room/internal/modes/assets"
	"github.com/an4eetos/decision-room/internal/modes/domain"
)

func registryWith(modes ...domain.Mode) domain.Registry {
	open := domain.Mode{ID: DefaultModeID, Name: "Open", Family: domain.FamilyOpen, SystemPrompt: "x"}
	return domain.Registry{Modes: append([]domain.Mode{open}, modes...)}
}

func TestExplicitOnlyModesNeedNoTriggers(t *testing.T) {
	t.Parallel()

	mode := domain.Mode{ID: "grill", Name: "Grill", Family: domain.FamilyInterrogate, SystemPrompt: "x", ExplicitOnly: true}
	if err := validate(registryWith(mode)); err != nil {
		t.Fatalf("explicit-only mode without triggers should pass: %v", err)
	}
}

func TestOrdinaryModesStillNeedTriggers(t *testing.T) {
	t.Parallel()

	mode := domain.Mode{ID: "plain", Name: "Plain", Family: domain.FamilyPlan, SystemPrompt: "x"}
	if err := validate(registryWith(mode)); err == nil || !strings.Contains(err.Error(), "trigger") {
		t.Fatalf("expected a missing-trigger error, got %v", err)
	}
}

func TestExplicitOnlyModesRejectDeadTriggers(t *testing.T) {
	t.Parallel()

	mode := domain.Mode{
		ID: "grill", Name: "Grill", Family: domain.FamilyInterrogate, SystemPrompt: "x", ExplicitOnly: true,
		Triggers: domain.Triggers{Keywords: []string{"grill me"}, Weight: 1},
	}
	if err := validate(registryWith(mode)); err == nil {
		t.Fatal("triggers on an explicit-only mode would never fire; expected an error")
	}
}

func TestInterrogationModesMustBeExplicitOnly(t *testing.T) {
	t.Parallel()

	mode := domain.Mode{
		ID: "grill", Name: "Grill", Family: domain.FamilyInterrogate, SystemPrompt: "x",
		Triggers: domain.Triggers{Keywords: []string{"grill me"}, Weight: 1},
	}
	if err := validate(registryWith(mode)); err == nil || !strings.Contains(err.Error(), "explicit_only") {
		t.Fatalf("expected an explicit_only error, got %v", err)
	}
}

func TestParsesThePositionSection(t *testing.T) {
	t.Parallel()

	data := []byte("---\nid: grill\nname: Grill\nfamily: interrogate\nexplicit_only: true\n---\n\n## System\n\nAsk.\n\n## Output\n\n**Questions**\n\n## Position\n\n**Position**\n")
	mode, err := parse(data, "test")
	if err != nil {
		t.Fatal(err)
	}
	if mode.ConcludePrompt != "**Position**" || mode.OutputPrompt != "**Questions**" {
		t.Fatalf("sections parsed wrong: output=%q position=%q", mode.OutputPrompt, mode.ConcludePrompt)
	}
}

// Nobody gets sworn at because a keyword matched.
func TestBeastModesMustBeExplicitOnlyAndRankShouters(t *testing.T) {
	t.Parallel()

	detectable := domain.Mode{
		ID: "rage", Name: "Rage", Family: domain.FamilyBeast, SystemPrompt: "x", Shouters: []string{"zhukov"},
		Triggers: domain.Triggers{Keywords: []string{"shout at me"}, Weight: 1},
	}
	if err := validate(registryWith(detectable)); err == nil || !strings.Contains(err.Error(), "explicit_only") {
		t.Fatalf("expected an explicit_only error, got %v", err)
	}

	unranked := domain.Mode{ID: "rage", Name: "Rage", Family: domain.FamilyBeast, SystemPrompt: "x", ExplicitOnly: true}
	if err := validate(registryWith(unranked)); err == nil || !strings.Contains(err.Error(), "shouters") {
		t.Fatalf("expected a shouters error, got %v", err)
	}

	ranked := unranked
	ranked.Shouters = []string{"zhukov", "patton"}
	if err := validate(registryWith(ranked)); err != nil {
		t.Fatalf("an explicit-only beast mode with a ranking should pass: %v", err)
	}
}

// The staff reads the map on request, never on a keyword; a red team is one
// general.
func TestStaffModesAreExplicitAndARedTeamIsOneGeneral(t *testing.T) {
	t.Parallel()

	detectable := domain.Mode{
		ID: "staff", Name: "Staff", Family: domain.FamilyStaff, SystemPrompt: "x",
		Triggers: domain.Triggers{Keywords: []string{"read the map"}, Weight: 1},
	}
	if err := validate(registryWith(detectable)); err == nil || !strings.Contains(err.Error(), "explicit_only") {
		t.Fatalf("expected an explicit_only error, got %v", err)
	}

	crowd := domain.Mode{
		ID: "red", Name: "Red", Family: domain.FamilyStaff, SystemPrompt: "x", ExplicitOnly: true, RedTeam: true,
		Generals: domain.Generals{Max: 3},
	}
	if err := validate(registryWith(crowd)); err == nil || !strings.Contains(err.Error(), "one general") {
		t.Fatalf("expected a one-general error, got %v", err)
	}

	one := crowd
	one.Generals.Max = 1
	if err := validate(registryWith(one)); err != nil {
		t.Fatalf("an explicit-only red team with one general should pass: %v", err)
	}
}

// The shipped roster carries both staff modes, with the red team flagged.
func TestShippedStaffModesLoad(t *testing.T) {
	t.Parallel()

	registry, err := Load(assets.Modes(), "")
	if err != nil {
		t.Fatal(err)
	}
	var staff, red *domain.Mode
	for i := range registry.Modes {
		switch registry.Modes[i].ID {
		case "staff":
			staff = &registry.Modes[i]
		case "red_team":
			red = &registry.Modes[i]
		}
	}
	if staff == nil || !staff.IsStaff() || staff.IsRedTeam() || strings.TrimSpace(staff.OutputPrompt) == "" {
		t.Fatalf("staff appraisal mode: %+v", staff)
	}
	if red == nil || !red.IsStaff() || !red.IsRedTeam() || red.Generals.Max != 1 || len(red.Generals.Default) == 0 {
		t.Fatalf("red team mode: %+v", red)
	}
}
