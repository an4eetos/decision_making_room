package service_test

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/modes/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/modes/assets"
	"github.com/an4eetos/decision-room/internal/modes/service"
)

func detector(t *testing.T) *service.Detector {
	t.Helper()
	reg, err := fs.Load(assets.Modes(), "")
	if err != nil {
		t.Fatalf("load modes: %v", err)
	}
	return service.NewDetector(fs.NewRegistry(reg))
}

func TestDetectsModeFromRealQuestions(t *testing.T) {
	t.Parallel()
	d := detector(t)

	cases := []struct {
		question string
		want     string
	}{
		{"Should I take the job in Berlin or stay where I am?", "hard_call"},
		{"I've been avoiding this migration for a week", "stalled"},
		{"Plan my day", "day_plan"},
		{"What could go wrong with the Bangkok move?", "pre_mortem"},
		{"I want to move to Portugal next year", "big_move"},
		{"Everything is urgent and I'm drowning", "overloaded"},
		{"How did today go?", "debrief"},
		{"Review my week", "weekly_review"},
		{"I need to ask for a raise", "conversation_prep"},
		{"This project is too big and will never ship", "scope_cut"},
		{"Poke holes in this plan for me", "second_opinion"},
		{"Bad week, I'm exhausted and nothing worked", "reset"},
		{"Plan my week", "week_shape"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()

			got := d.Detect(service.Input{Question: tc.question})
			if got.Mode.ID != tc.want {
				t.Fatalf("%q detected as %q (via %s), want %q",
					tc.question, got.Mode.ID, got.Method, tc.want)
			}
		})
	}
}

// An ordinary question should not be forced into a template.
func TestFallsBackToOpen(t *testing.T) {
	t.Parallel()
	d := detector(t)

	for _, q := range []string{
		"What did I decide about the database?",
		"Remind me what pgvector does",
		"",
	} {
		got := d.Detect(service.Input{Question: q})
		if got.Mode.ID != "open" {
			t.Fatalf("%q detected as %q; an ordinary question should stay open", q, got.Mode.ID)
		}
		if got.Method != service.MethodDefault {
			t.Fatalf("method = %q, want default", got.Method)
		}
	}
}

func TestExplicitPickWins(t *testing.T) {
	t.Parallel()

	got := detector(t).Detect(service.Input{
		Question: "I've been avoiding this for a week",
		Explicit: "pre_mortem",
	})
	if got.Mode.ID != "pre_mortem" || got.Method != service.MethodExplicit {
		t.Fatalf("got %q via %s", got.Mode.ID, got.Method)
	}
}

// Changing the shape of the answer underneath someone mid-conversation is worse
// than staying in a slightly wrong mode.
func TestStaysInModeOnWeakSignal(t *testing.T) {
	t.Parallel()

	got := detector(t).Detect(service.Input{
		Question:    "and what about the second option",
		SessionMode: "hard_call",
		TurnIndex:   3,
	})
	if got.Mode.ID != "hard_call" || got.Method != service.MethodSticky {
		t.Fatalf("got %q via %s, want hard_call via sticky", got.Mode.ID, got.Method)
	}
}

// A clear change of subject should still switch.
func TestSwitchesOnStrongSignal(t *testing.T) {
	t.Parallel()

	got := detector(t).Detect(service.Input{
		Question:    "Forget that. Plan my day — what should I do today?",
		SessionMode: "hard_call",
		TurnIndex:   3,
	})
	if got.Mode.ID != "day_plan" {
		t.Fatalf("got %q via %s, want day_plan", got.Mode.ID, got.Method)
	}
}

// A mode set by hand is never overridden by detection, however strong.
func TestLockedModeIsNeverOverridden(t *testing.T) {
	t.Parallel()

	got := detector(t).Detect(service.Input{
		Question:    "plan my day, what should I do today",
		SessionMode: "pre_mortem",
		Locked:      true,
		TurnIndex:   2,
	})
	if got.Mode.ID != "pre_mortem" {
		t.Fatalf("got %q; a locked mode must stick", got.Mode.ID)
	}
}

// Stickiness must not apply to the first turn, or every conversation would
// begin in whatever mode the session was created with.
func TestFirstTurnIsNotSticky(t *testing.T) {
	t.Parallel()

	got := detector(t).Detect(service.Input{
		Question:    "plan my day",
		SessionMode: "pre_mortem",
		TurnIndex:   0,
	})
	if got.Mode.ID != "day_plan" {
		t.Fatalf("got %q, want day_plan on the first turn", got.Mode.ID)
	}
}

func TestDetectionIsDeterministic(t *testing.T) {
	t.Parallel()
	d := detector(t)

	in := service.Input{Question: "should I cut scope or keep going"}
	first := d.Detect(in).Mode.ID
	for i := 0; i < 5; i++ {
		if got := d.Detect(in).Mode.ID; got != first {
			t.Fatalf("detection varied: %q then %q", first, got)
		}
	}
}

func TestUnknownExplicitModeFallsThrough(t *testing.T) {
	t.Parallel()

	got := detector(t).Detect(service.Input{Question: "plan my day", Explicit: "nonsense"})
	if got.Mode.ID != "day_plan" {
		t.Fatalf("got %q; an unknown explicit mode should fall through to detection", got.Mode.ID)
	}
}
