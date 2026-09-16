package fs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// The card is the whole reason this design exists: injecting every general's
// full doctrine is what made the original cost thousands of tokens per question.
func TestCardsStaySmallAndDoctrineStaysOut(t *testing.T) {
	t.Parallel()

	roster := load(t, "")
	for _, lens := range roster.Generals {
		card := lens.Card()

		if len(lens.Doctrine) == 0 {
			t.Fatalf("%s: doctrine body was not captured", lens.ID)
		}
		if strings.Contains(card, lens.Doctrine) {
			t.Fatalf("%s: full doctrine leaked into the card", lens.ID)
		}
		// Roughly four characters per token; 700 chars is about 175 tokens.
		if len(card) > 700 {
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

	roster := load(t, "")
	block := domain.Cards(roster.Generals[:3])

	// The original injected all nine generals in full on every question, about
	// 19KB. Three cards must be a rounding error against that.
	if len(block) > 2200 {
		t.Fatalf("three cards are %d chars; the point was to be cheap", len(block))
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
