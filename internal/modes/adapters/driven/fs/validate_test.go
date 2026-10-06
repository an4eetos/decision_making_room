package fs

import (
	"strings"
	"testing"

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
