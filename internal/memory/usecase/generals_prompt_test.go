package usecase

import (
	"strings"
	"testing"

	genfs "github.com/an4eetos/decision-room/internal/generals/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/generals/assets"
	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
)

func roster(t *testing.T) []gendomain.Lens {
	t.Helper()
	r, err := genfs.Load(assets.Generals(), assets.Styles(), "")
	if err != nil {
		t.Fatalf("load roster: %v", err)
	}
	return r.Generals
}

// The whole reason lenses are selected rather than all injected. The original
// put every general's full doctrine into every prompt — about 5,000 tokens of a
// ~12,000 token always-on context block, regardless of what was asked.
func TestGeneralsPromptStaysCheap(t *testing.T) {
	t.Parallel()
	lenses := roster(t)

	// Roughly four characters per token.
	if got := len(generalsPrompt(lenses[:1], false)); got > 1600 {
		t.Fatalf("one lens costs %d chars (~%d tokens); it should be a few hundred", got, got/4)
	}
	if got := len(generalsPrompt(lenses[:3], false)); got > 4000 {
		t.Fatalf("three lenses cost %d chars (~%d tokens)", got, got/4)
	}

	full := 0
	for _, l := range lenses {
		full += len(l.Doctrine)
	}
	if len(generalsPrompt(lenses[:3], false)) >= full {
		t.Fatal("three cards should cost far less than the roster's full doctrine")
	}
}

func TestGeneralsPromptNeverLeaksDoctrine(t *testing.T) {
	t.Parallel()
	lenses := roster(t)

	prompt := generalsPrompt(lenses[:3], false)
	for _, lens := range lenses[:3] {
		// Compare on a distinctive slice; the doctrine is long and multi-line.
		if probe := firstSentence(lens.Doctrine); probe != "" && strings.Contains(prompt, probe) {
			t.Fatalf("%s: doctrine body reached the prompt", lens.ID)
		}
	}
}

// Without the anti-consensus instruction three lenses converge into polite
// agreement and the feature is worth nothing over a single voice.
func TestMultiLensPromptDemandsDisagreement(t *testing.T) {
	t.Parallel()
	lenses := roster(t)

	prompt := generalsPrompt(lenses[:3], false)
	for _, want := range []string{"Where they disagree", "The call", "Do not soften disagreement"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("multi-lens prompt missing %q", want)
		}
	}
	for _, lens := range lenses[:3] {
		if !strings.Contains(prompt, "## "+lens.Name) {
			t.Fatalf("missing a section for %s", lens.Name)
		}
	}
}

// One lens gets a single voice, not an argument with itself.
func TestSingleLensPromptHasNoSections(t *testing.T) {
	t.Parallel()

	prompt := generalsPrompt(roster(t)[:1], false)
	if strings.Contains(prompt, "Where they disagree") {
		t.Fatal("a single lens should not be asked to disagree with itself")
	}
	if !strings.Contains(prompt, "blind spot") {
		t.Fatal("a single lens should still be asked to name its blind spot")
	}
}

func TestNoLensesMeansNoPrompt(t *testing.T) {
	t.Parallel()

	if got := generalsPrompt(nil, false); got != "" {
		t.Fatalf("expected an empty prompt, got %q", got)
	}
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '.'); i > 20 {
		return s[:i]
	}
	return ""
}

// A mode that already dictates sections must not get a second set from the
// lenses; the two templates were previously concatenated into one answer.
func TestStructuredModeSuppressesLensSections(t *testing.T) {
	t.Parallel()
	lenses := roster(t)

	prompt := generalsPrompt(lenses[:3], true)

	for _, lens := range lenses[:3] {
		if strings.Contains(prompt, "## "+lens.Name) {
			t.Fatalf("lens sections leaked into a mode that owns its structure (%s)", lens.Name)
		}
	}
	if strings.Contains(prompt, "Where they disagree") {
		t.Fatal("a structured mode should not get its own disagreement section")
	}
	// The disagreement still has to happen, just inside the mode's sections.
	if !strings.Contains(prompt, "Do not soften disagreement") {
		t.Fatal("the anti-consensus instruction must survive")
	}
	if !strings.Contains(prompt, "Keep the section structure") {
		t.Fatal("expected the lenses to be told to respect the mode's structure")
	}
}
