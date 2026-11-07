package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPassagesSplitOnSections(t *testing.T) {
	t.Parallel()

	lens := Lens{ID: "x", Name: "X", Doctrine: "Preamble.\n\n## Doctrine\n\nFirst.\n\n## The case against\n\nSecond."}
	got := lens.Passages()

	want := []struct{ section, text string }{{"", "Preamble."}, {"Doctrine", "First."}, {"The case against", "Second."}}
	if len(got) != len(want) {
		t.Fatalf("got %d passages, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Section != w.section || got[i].Text != w.text {
			t.Fatalf("passage %d = %q/%q, want %q/%q", i, got[i].Section, got[i].Text, w.section, w.text)
		}
		if got[i].LensID != "x" {
			t.Fatalf("passage %d lost its lens id", i)
		}
	}
}

// A long section splits between paragraphs, never inside one: half a thought in
// a prompt is worse than a passage that runs a little long.
func TestLongSectionsSplitAtParagraphs(t *testing.T) {
	t.Parallel()

	para := strings.Repeat("word ", 100) // 500 runes
	body := "## Facing the unknown\n\n" + para + "\n\n" + para + "\n\n" + para
	passages := Lens{ID: "x", Name: "X", Doctrine: body}.Passages()

	if len(passages) < 2 {
		t.Fatalf("a %d-rune section should split, got %d passage", len(body), len(passages))
	}
	for _, p := range passages {
		if p.Section != "Facing the unknown" {
			t.Fatalf("split passage lost its section: %q", p.Section)
		}
		if n := utf8.RuneCountInString(p.Text); n > MaxPassageRunes {
			t.Fatalf("passage is %d runes, over %d", n, MaxPassageRunes)
		}
		if strings.Count(p.Text, strings.TrimSpace(para)) == 0 {
			t.Fatal("a paragraph was cut in half")
		}
	}
}

func TestEmbedTextCarriesNameAndSection(t *testing.T) {
	t.Parallel()

	p := Passage{LensName: "Zhukov", Section: "Where it broke", Text: "Seelow."}
	if got := p.EmbedText(); !strings.HasPrefix(got, "Zhukov — Where it broke") {
		t.Fatalf("embed text should lead with name and section, got %q", got)
	}
}
