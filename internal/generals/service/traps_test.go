package service_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/an4eetos/decision-room/internal/generals/service"
)

func hitIDs(hits []service.TrapHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return out
}

func TestDetectsTrapsInRealSentences(t *testing.T) {
	t.Parallel()
	traps := registry(t).Traps()

	cases := []struct {
		text string
		want []string
	}{
		{"I want to quit but everyone at work will think I'm a failure.", []string{"spotlight", "all_or_nothing"}},
		{"I already put three years into this company.", []string{"sunk_cost"}},
		{"Honestly I have no choice, I just have to wait for them.", []string{"encircled"}},
		{"It's probably fine, let's see how next month goes.", []string{"normalcy"}},
		{"If she says no I'll never get over it.", []string{"impact_bias"}},
		{"They'll take it the wrong way if I bring it up.", []string{"transparency"}},
		{"If this launch slips it's a disaster and I lose everything.", []string{"catastrophizing"}},
		{"I can't give up now, I just need to push harder.", []string{"escalation"}},
		{"Is it selfish to move abroad and leave my parents?", []string{"permission_seeking"}},
		{"I need more information before I can decide anything.", []string{"analysis_paralysis"}},
		{"I'm scared of losing my apartment if I take the job.", []string{"loss_aversion"}},
		{"Better the devil you know, right?", []string{"status_quo"}},
		// Curly apostrophes, as phones type them.
		{"I can’t handle another rejection.", []string{"impact_bias"}},
		// A weak trap rides along with a strong one.
		{"I'll try to talk to him at some point, but I'd look stupid.", []string{"spotlight", "vague_intent"}},
	}

	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			t.Parallel()
			got := hitIDs(service.DetectTraps(tc.text, traps))
			for _, want := range tc.want {
				if !slices.Contains(got, want) {
					t.Fatalf("%q: got %v, want it to include %q", tc.text, got, want)
				}
			}
		})
	}
}

// The detector decides whether to interrupt someone. A false alarm on ordinary
// speech costs more than a missed trap, because a room that cries wolf is
// ignored.
func TestDoesNotFireOnOrdinarySpeech(t *testing.T) {
	t.Parallel()
	traps := registry(t).Traps()

	quiet := []string{
		"Plan my day.",
		"Nobody noticed and it was fine.",
		"I'm not embarrassed about it at all.",
		"What should I cook tonight?",
		"Summarise what I decided about the database.",
		"I'll try to finish the report by Friday.",
		"Obviously the meeting moved to Tuesday.",
		"We visited the ruins of an old castle.",
		"",
	}
	for _, text := range quiet {
		if hits := service.DetectTraps(text, traps); len(hits) > 0 {
			t.Errorf("%q: unexpected traps %v", text, hitIDs(hits))
		}
	}
}

func TestQuotesTheSentenceThatGaveItAway(t *testing.T) {
	t.Parallel()

	text := "Work is fine. But everyone will think I gave up! Anyway."
	hits := service.DetectTraps(text, registry(t).Traps())
	if len(hits) == 0 {
		t.Fatal("expected a spotlight hit")
	}
	if hits[0].Quote != "But everyone will think I gave up!" {
		t.Fatalf("quote = %q", hits[0].Quote)
	}
	if hits[0].Name == "" {
		t.Fatal("hit should carry the trap's display name")
	}
}

func TestLongQuotesAreCut(t *testing.T) {
	t.Parallel()

	text := "I would look stupid " + strings.Repeat("and keep going ", 40)
	hits := service.DetectTraps(text, registry(t).Traps())
	if len(hits) == 0 {
		t.Fatal("expected a hit")
	}
	if n := len([]rune(hits[0].Quote)); n > 160 {
		t.Fatalf("quote is %d runes, want at most 160", n)
	}
}
