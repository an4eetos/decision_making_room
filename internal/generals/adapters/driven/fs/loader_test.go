package fs_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/an4eetos/decision-room/internal/generals/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/generals/assets"
	"github.com/an4eetos/decision-room/internal/generals/domain"
)

func load(t *testing.T, overlay string) domain.Roster {
	t.Helper()
	roster, err := fs.Load(assets.Generals(), assets.Styles(), overlay)
	if err != nil {
		t.Fatalf("load roster: %v", err)
	}
	return roster
}

func TestBuiltinRosterIsValid(t *testing.T) {
	t.Parallel()

	roster := load(t, "")
	if len(roster.Generals) != 20 {
		t.Fatalf("expected 20 generals, got %d", len(roster.Generals))
	}
	if len(roster.Styles) != 13 {
		t.Fatalf("expected 13 working styles, got %d", len(roster.Styles))
	}
}

// Selection seats every rival of the leading lens. Two rivals from the same
// family fill the slots backfill would have spent on a third family, and the
// exchange loses the side it most needed.
func TestBuiltinRivalsAreFromDistinctFamilies(t *testing.T) {
	t.Parallel()

	roster := load(t, "")
	byID := make(map[string]domain.Lens, len(roster.Generals))
	for _, g := range roster.Generals {
		byID[g.ID] = g
	}
	for _, g := range roster.Generals {
		if len(g.Rivals) == 0 || len(g.Asks) == 0 || len(g.ConcedesWhen) == 0 || g.Unknowns == "" {
			t.Fatalf("%s: shipped generals need asks, unknowns, concedes_when and rivals", g.ID)
		}
		seen := map[domain.Family]string{g.Family: g.ID}
		for _, id := range g.Rivals {
			family := byID[id].Family
			if prev, dup := seen[family]; dup {
				t.Fatalf("%s: rival %s shares family %q with %s", g.ID, id, family, prev)
			}
			seen[family] = id
		}
	}
}

// The card is the whole reason this design exists: injecting every general's
// full doctrine is what made the original cost thousands of tokens per question.
func TestCardsStaySmallAndDoctrineStaysOut(t *testing.T) {
	t.Parallel()

	// Measured through the registry, which resolves rival names: the served
	// card carries an "Argues most with" line the raw roster does not, and
	// measuring without it let cards over the limit pass.
	for _, lens := range fs.NewRegistry(load(t, "")).Generals() {
		card := lens.Card()

		if len(lens.Doctrine) == 0 {
			t.Fatalf("%s: doctrine body was not captured", lens.ID)
		}
		if strings.Contains(card, lens.Doctrine) {
			t.Fatalf("%s: full doctrine leaked into the card", lens.ID)
		}
		// Roughly four characters per token; 1300 chars is about 325 tokens. The
		// dialectical fields (asks, concedes, rivals) are what push a card past
		// the original 700: worth it, but bounded.
		if len(card) > 1300 {
			t.Fatalf("%s: card is %d chars, too large to inject three of", lens.ID, len(card))
		}
		for _, want := range []string{lens.Name, "Job:", "Blind spot:"} {
			if !strings.Contains(card, want) {
				t.Fatalf("%s: card missing %q", lens.ID, want)
			}
		}
	}
}

func TestThreeCardsFitTheBudget(t *testing.T) {
	t.Parallel()

	// Measure the worst case — the three largest cards, as served — not
	// whichever three happen to sort first.
	cards := append([]domain.Lens(nil), fs.NewRegistry(load(t, "")).Generals()...)
	sort.Slice(cards, func(i, j int) bool { return len(cards[i].Card()) > len(cards[j].Card()) })
	block := domain.Cards(cards[:3])

	// The original injected all nine generals in full on every question, about
	// 19KB. Three cards must stay a small fraction of that.
	if len(block) > 4000 {
		t.Fatalf("the three largest cards are %d chars; the point was to be cheap", len(block))
	}
}

func TestOverlayReplacesAndAppends(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	generalsDir := filepath.Join(dir, "generals")
	if err := os.MkdirAll(generalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `---
id: zhukov
name: Zhukov
family: endurance
job: My own version.
deploy_when: [always]
bias: none stated
routes:
  keywords: [grind]
---
Body.
`
	custom := strings.Replace(doc, "id: zhukov", "id: my_own", 1)
	custom = strings.Replace(custom, "name: Zhukov", "name: My Own", 1)

	if err := os.WriteFile(filepath.Join(generalsDir, "zhukov.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generalsDir, "mine.md"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	roster := load(t, dir)

	var replaced, appended int
	for _, lens := range roster.Generals {
		if lens.ID == "zhukov" {
			replaced++
			if lens.Job != "My own version." {
				t.Fatalf("overlay did not replace zhukov: %q", lens.Job)
			}
		}
		if lens.ID == "my_own" {
			appended++
		}
	}
	if replaced != 1 {
		t.Fatalf("zhukov appears %d times; replacement must not duplicate", replaced)
	}
	if appended != 1 {
		t.Fatal("overlay did not append the new general")
	}
}

// A lens with no blind spot still renders a card, so nothing downstream can tell
// it is degraded. Failing at startup is the only place it can be caught.
func TestValidationRejectsMissingBias(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	generalsDir := filepath.Join(dir, "generals")
	if err := os.MkdirAll(generalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `---
id: incomplete
name: Incomplete
family: contact
job: Does something.
deploy_when: [sometimes]
routes:
  keywords: [thing]
---
`
	if err := os.WriteFile(filepath.Join(generalsDir, "bad.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Load(assets.Generals(), assets.Styles(), dir); err == nil {
		t.Fatal("expected a general with no bias to fail validation")
	} else if !strings.Contains(err.Error(), "bias") {
		t.Fatalf("error should name the missing field, got: %v", err)
	}
}

func TestValidationRejectsUnknownFamily(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	generalsDir := filepath.Join(dir, "generals")
	if err := os.MkdirAll(generalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `---
id: odd
name: Odd
family: logistics
job: Does something.
deploy_when: [sometimes]
bias: unclear
routes:
  keywords: [thing]
---
`
	if err := os.WriteFile(filepath.Join(generalsDir, "bad.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Load(assets.Generals(), assets.Styles(), dir); err == nil {
		t.Fatal("expected an unknown family to fail validation")
	}
}

func TestParseRejectsMissingFrontmatter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	generalsDir := filepath.Join(dir, "generals")
	if err := os.MkdirAll(generalsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generalsDir, "bad.md"), []byte("# Just markdown\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Load(assets.Generals(), assets.Styles(), dir); err == nil {
		t.Fatal("expected a file without frontmatter to fail")
	}
}

// A passage is the unit that reaches a prompt on every tier, so it is what a
// doctrine costs. Long sections are fine; they split at paragraph breaks. A
// single paragraph over the limit cannot split, and should be rewritten.
func TestDoctrinePassagesStayBounded(t *testing.T) {
	t.Parallel()

	roster := load(t, "")
	for _, lens := range roster.Generals {
		passages := lens.Passages()
		if len(passages) == 0 {
			t.Fatalf("%s: doctrine produced no passages", lens.ID)
		}
		for _, p := range passages {
			if p.Section == "" {
				t.Fatalf("%s: text before the first ## heading has no section to be found by", lens.ID)
			}
			if n := utf8.RuneCountInString(p.Text); n > domain.MaxPassageRunes {
				t.Fatalf("%s / %s: a %d-rune paragraph cannot be split under %d; break it up",
					lens.ID, p.Section, n, domain.MaxPassageRunes)
			}
		}
	}
}

// Modes prefer sections by heading ("In the pocket" for an overloaded week,
// "The case against" for a pre-mortem), and the lenses are compared on the
// same situations. A general missing one silently drops out of that comparison.
func TestEveryGeneralHasTheStandardSections(t *testing.T) {
	t.Parallel()

	standard := []string{
		"Doctrine", "In the pocket", "With reserves in hand", "The case against",
		"Where it broke", "Rivals", "Over a long game", "Facing the unknown",
	}
	for _, lens := range load(t, "").Generals {
		have := map[string]bool{}
		for _, p := range lens.Passages() {
			have[p.Section] = true
		}
		for _, want := range standard {
			if !have[want] {
				t.Errorf("%s: missing the %q section", lens.ID, want)
			}
		}
	}
}
