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
	r, err := genfs.Load(assets.Generals(), assets.Styles(), assets.Traps(), "")
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

	// Roughly four characters per token. Bounds include the debate protocol's
	// instructions, which is most of what a single lens costs.
	if got := len(generalsPrompt(lenses[:1], nil, false)); got > 2000 {
		t.Fatalf("one lens costs %d chars (~%d tokens); it should be a few hundred", got, got/4)
	}
	if got := len(generalsPrompt(lenses[:3], nil, false)); got > 6000 {
		t.Fatalf("three lenses cost %d chars (~%d tokens)", got, got/4)
	}

	full := 0
	for _, l := range lenses {
		full += len(l.Doctrine)
	}
	if len(generalsPrompt(lenses[:3], nil, false)) >= full {
		t.Fatal("three cards should cost far less than the roster's full doctrine")
	}
}

func TestGeneralsPromptNeverLeaksDoctrine(t *testing.T) {
	t.Parallel()
	lenses := roster(t)

	prompt := generalsPrompt(lenses[:3], nil, false)
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

	prompt := generalsPrompt(lenses[:3], nil, false)
	for _, want := range []string{"## The fork", "## The call", "**Answers", "**Concedes:**", "Do not soften disagreement"} {
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

	prompt := generalsPrompt(roster(t)[:1], nil, false)
	if strings.Contains(prompt, "The fork") {
		t.Fatal("a single lens should not be asked to disagree with itself")
	}
	if !strings.Contains(prompt, "blind spot") {
		t.Fatal("a single lens should still be asked to name its blind spot")
	}
}

func TestNoLensesMeansNoPrompt(t *testing.T) {
	t.Parallel()

	if got := generalsPrompt(nil, nil, false); got != "" {
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

	prompt := generalsPrompt(lenses[:3], nil, true)

	for _, lens := range lenses[:3] {
		if strings.Contains(prompt, "## "+lens.Name) {
			t.Fatalf("lens sections leaked into a mode that owns its structure (%s)", lens.Name)
		}
	}
	if strings.Contains(prompt, "## The fork") {
		t.Fatal("a structured mode should not get its own fork section")
	}
	// The disagreement still has to happen, just inside the mode's sections.
	if !strings.Contains(prompt, "Do not soften disagreement") {
		t.Fatal("the anti-consensus instruction must survive")
	}
	if !strings.Contains(prompt, "Keep the section structure") {
		t.Fatal("expected the lenses to be told to respect the mode's structure")
	}
}

// The exchange has to be an exchange: each lens answers the strongest opposing
// point and concedes something real, even inside a mode's own structure.
func TestStructuredModeStillDemandsRebuttals(t *testing.T) {
	t.Parallel()

	prompt := generalsPrompt(roster(t)[:2], nil, true)
	if !strings.Contains(prompt, "strongest point") || !strings.Contains(prompt, "concession states what the other side gets right") {
		t.Fatal("structured modes must still require rebuttals and real concessions")
	}
}

// A passage has to sit under its own lens's card; attributed to the wrong lens
// it would have one general arguing another's doctrine.
func TestDoctrinePassagesRenderUnderTheirCard(t *testing.T) {
	t.Parallel()
	lenses := roster(t)[:2]
	passage := lenses[1].Passages()[0]

	prompt := generalsPrompt(lenses, map[string][]gendomain.Passage{lenses[1].ID: {passage}}, false)

	at := strings.Index(prompt, passage.Text)
	if at < 0 {
		t.Fatal("the chosen passage did not reach the prompt")
	}
	if strings.Index(prompt, lenses[1].Card()) > at || strings.Index(prompt, lenses[0].Card()) > at {
		t.Fatal("the passage should follow its own card, after the other lens's")
	}
	if !strings.Contains(prompt, "outranks anything you know about the historical figure") {
		t.Fatal("the model should be told to argue from the passage")
	}
	// Only the chosen passage: the rest of the doctrine stays out.
	for _, other := range lenses[1].Passages()[1:] {
		if other.Text != passage.Text && strings.Contains(prompt, other.Text) {
			t.Fatalf("an unchosen passage (%s) leaked into the prompt", other.Section)
		}
	}
}

// Doctrine on every tier is only affordable while a passage stays small. Three
// lenses with two passages each must still be a fraction of the old ~19KB.
func TestGeneralsPromptWithDoctrineStaysCheap(t *testing.T) {
	t.Parallel()
	lenses := roster(t)

	one := map[string][]gendomain.Passage{lenses[0].ID: lenses[0].Passages()[:1]}
	if got := len(generalsPrompt(lenses[:1], one, false)); got > 3200 {
		t.Fatalf("one lens with a passage costs %d chars (~%d tokens)", got, got/4)
	}

	deep := map[string][]gendomain.Passage{}
	for _, l := range lenses[:3] {
		deep[l.ID] = l.Passages()[:2]
	}
	if got := len(generalsPrompt(lenses[:3], deep, false)); got > 12000 {
		t.Fatalf("three lenses with two passages each cost %d chars (~%d tokens)", got, got/4)
	}
}

// "Frames, not characters" flattened every lens into one polite voice. A lens
// built on pressure has to arrive with its pressure intact.
func TestLensesKeepTheirRegister(t *testing.T) {
	t.Parallel()

	for _, n := range []int{1, 3} {
		prompt := generalsPrompt(roster(t)[:n], nil, false)
		if !strings.Contains(prompt, "keeps the register of its \"Sounds like\" line") {
			t.Fatalf("%d lens(es): the register instruction is missing", n)
		}
		if !strings.Contains(prompt, "do not write in period voice") {
			t.Fatalf("%d lens(es): keeping the register must not lift the period-voice ban", n)
		}
	}
}
