package service

import (
	"testing"
	"time"
)

func TestFingerprintCollapsesRephrasings(t *testing.T) {
	t.Parallel()

	same := []string{
		"Ship the MVP by Friday",
		"ship MVP by friday!",
		"I'll ship the MVP by Friday",
		"by Friday, ship the MVP",
	}
	want := Fingerprint(same[0])
	for _, s := range same[1:] {
		if got := Fingerprint(s); got != want {
			t.Fatalf("%q fingerprinted differently from %q", s, same[0])
		}
	}
}

func TestFingerprintKeepsDifferentCommitmentsApart(t *testing.T) {
	t.Parallel()

	if Fingerprint("ship the MVP by Friday") == Fingerprint("ship the MVP by Monday") {
		t.Fatal("different deadlines must not collapse")
	}
	if Fingerprint("finish the relocation UI") == Fingerprint("finish the chat UI") {
		t.Fatal("different objects must not collapse")
	}
}

func TestPrefilterCatchesFirstPersonPromises(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"I'll ship it by Friday",
		"OK, I will finish the migration tomorrow",
		"I'm going to call the landlord",
		"I need to book the flight",
		"The plan is to launch next week",
	} {
		if !LooksLikeCommitment(s) {
			t.Errorf("missed a commitment: %q", s)
		}
	}
}

// The prefilter exists to save a model call on turns that obviously contain no
// promise. Questions and reflections are the common case.
func TestPrefilterSkipsQuestionsAndReflection(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"What did I decide about the database?",
		"How did today go?",
		"Everything is urgent and I'm drowning",
		"I'm willing to consider it",      // "will" inside "willing"
		"that was goodwill on their part", // "will" inside "goodwill"
	} {
		if LooksLikeCommitment(s) {
			t.Errorf("false positive, would waste a model call: %q", s)
		}
	}
}

func TestParseCandidatesAppliesConfidenceFloor(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	answer := `[
		{"text": "ship the MVP", "due": "2026-09-30", "confidence": 0.9},
		{"text": "maybe look at it", "due": null, "confidence": 0.3}
	]`

	got, err := ParseCandidates(answer, now)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 1 || got[0].Text != "ship the MVP" {
		t.Fatalf("got %+v, want only the confident one", got)
	}
	if got[0].Due == nil || got[0].Due.Format("2006-01-02") != "2026-09-30" {
		t.Fatalf("due not parsed: %v", got[0].Due)
	}
	// "By the 30th" means by the end of it, not its first second.
	if got[0].Due.Hour() != 23 {
		t.Fatalf("due should be end of day, got %v", got[0].Due)
	}
}

func TestParseCandidatesToleratesFencesAndProse(t *testing.T) {
	t.Parallel()

	answer := "Sure:\n```json\n[{\"text\":\"call the landlord\",\"due\":null,\"confidence\":0.8}]\n```"
	got, err := ParseCandidates(answer, time.Now())
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
}

// "No commitments" is the most common answer and must not be an error.
func TestParseCandidatesEmptyIsNotAnError(t *testing.T) {
	t.Parallel()

	for _, answer := range []string{"", "[]", "  [ ]  "} {
		got, err := ParseCandidates(answer, time.Now())
		if err != nil || len(got) != 0 {
			t.Fatalf("answer %q: got %+v, err %v", answer, got, err)
		}
	}
}

// A deadline already in the past is almost always a misparse, and a commitment
// that arrives overdue is noise.
func TestParseCandidatesDropsPastDueDates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	got, _ := ParseCandidates(`[{"text":"x thing","due":"2026-09-01","confidence":0.9}]`, now)
	if len(got) != 1 || got[0].Due != nil {
		t.Fatalf("past due date should be dropped, got %+v", got)
	}
}

func TestParseCandidatesCapsCount(t *testing.T) {
	t.Parallel()

	answer := `[
		{"text":"one","confidence":0.9},{"text":"two","confidence":0.9},
		{"text":"three","confidence":0.9},{"text":"four","confidence":0.9},
		{"text":"five","confidence":0.9}
	]`
	got, _ := ParseCandidates(answer, time.Now())
	if len(got) > maxCandidates {
		t.Fatalf("got %d, capped at %d", len(got), maxCandidates)
	}
}
