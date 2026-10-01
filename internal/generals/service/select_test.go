package service_test

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/generals/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/generals/assets"
	"github.com/an4eetos/decision-room/internal/generals/port"
	"github.com/an4eetos/decision-room/internal/generals/service"
)

func registry(t *testing.T) port.Registry {
	t.Helper()
	roster, err := fs.Load(assets.Generals(), assets.Styles(), "")
	if err != nil {
		t.Fatalf("load roster: %v", err)
	}
	return fs.NewRegistry(roster)
}

func ids(sel service.Selection) []string {
	out := make([]string, 0, len(sel.Lenses))
	for _, l := range sel.Lenses {
		out = append(out, l.ID)
	}
	return out
}

func TestRoutesRealQuestionsToSensibleGenerals(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	cases := []struct {
		question string
		want     string
	}{
		{"I've been putting off this migration for a week and can't start", "patton"},
		{"Should I take the job in Berlin or stay where I am?", "sun_tzu"},
		{"Everything is urgent and I have too many meetings", "eisenhower"},
		{"I want to build a daily habit of practising", "suvorov"},
		{"My effort is scattered across five projects", "guderian"},
		{"I failed the launch and I feel like giving up entirely", "slim"},
		{"The tests are failing and I don't know why", "isolator"},
		{"How do I keep showing up every day for months", "zhukov"},
		{"Should I wait before committing to this or act now", "kutuzov"},
		{"I keep doing this same setup from scratch, need a template", "shaposhnikov"},
	}

	for _, tc := range cases {
		t.Run(tc.question[:28], func(t *testing.T) {
			t.Parallel()

			got := ids(service.Select(reg, service.SelectInput{Question: tc.question, Max: 3}))
			// isolator is a working style, so it must NOT be auto-selected as a
			// general — that case asserts the rosters stay separate.
			if tc.want == "isolator" {
				for _, id := range got {
					if id == "isolator" {
						t.Fatalf("a working style was selected as a general: %v", got)
					}
				}
				return
			}
			for _, id := range got {
				if id == tc.want {
					return
				}
			}
			t.Fatalf("expected %q among the selection, got %v", tc.want, got)
		})
	}
}

func TestExplicitPickWins(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	sel := service.Select(reg, service.SelectInput{
		Question: "I can't start this migration, totally frozen",
		Explicit: []string{"kutuzov", "giap"},
		Max:      3,
	})

	if sel.Method != "explicit" {
		t.Fatalf("method = %q, want explicit", sel.Method)
	}
	got := ids(sel)
	if len(got) != 2 || got[0] != "kutuzov" || got[1] != "giap" {
		t.Fatalf("explicit pick not honoured: %v", got)
	}
}

func TestExplicitPickIsCappedAtMax(t *testing.T) {
	t.Parallel()

	sel := service.Select(registry(t), service.SelectInput{
		Question: "anything",
		Explicit: []string{"patton", "zhukov", "kutuzov", "giap", "boyd"},
		Max:      3,
	})
	if len(sel.Lenses) != 3 {
		t.Fatalf("expected the pick capped to 3, got %d", len(sel.Lenses))
	}
}

// A saved session can hold an id that an overlay later removed. That must not
// silently substitute a lens the user never chose.
func TestUnknownExplicitIdsFallBackToAuto(t *testing.T) {
	t.Parallel()

	sel := service.Select(registry(t), service.SelectInput{
		Question: "everything is urgent and I'm overloaded",
		Explicit: []string{"napoleon", "caesar"},
		Max:      2,
	})
	if sel.Method != "auto" {
		t.Fatalf("method = %q, want auto when every explicit id is unknown", sel.Method)
	}
	if len(sel.Lenses) == 0 {
		t.Fatal("expected auto-selection to produce something")
	}
}

func TestPartiallyValidExplicitPickKeepsWhatResolved(t *testing.T) {
	t.Parallel()

	sel := service.Select(registry(t), service.SelectInput{
		Question: "anything",
		Explicit: []string{"napoleon", "zhukov"},
		Max:      3,
	})
	if sel.Method != "explicit" || len(sel.Lenses) != 1 || sel.Lenses[0].ID != "zhukov" {
		t.Fatalf("expected the one valid id kept, got %v via %s", ids(sel), sel.Method)
	}
}

// Among lenses that score the same, the one used last turn loses. Scores move in
// keyword-sized steps, so this is a tie-break rather than forced rotation — which
// is what stops one lens answering everything without overriding real evidence.
func TestRecentUseBreaksTies(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	// Two seeded defaults, no keyword evidence for either: an exact tie.
	in := service.SelectInput{Question: "nothing in particular", Defaults: []string{"konev", "zhukov"}, Max: 1}

	fresh := ids(service.Select(reg, in))
	if len(fresh) == 0 {
		t.Fatal("expected a selection")
	}

	in.RecentlyUsed = fresh
	demoted := ids(service.Select(reg, in))
	if len(demoted) == 0 {
		t.Fatal("expected a selection")
	}
	if demoted[0] == fresh[0] {
		t.Fatalf("a tie should go to the lens not used last turn; %q won twice", fresh[0])
	}
}

// The penalty must not override strong evidence. A question that is squarely
// about one lens should keep getting that lens.
func TestRecentUseDoesNotOverrideStrongEvidence(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	question := "I am frozen, cannot start, procrastinating, avoiding the blank page"

	got := ids(service.Select(reg, service.SelectInput{
		Question: question, Max: 1, RecentlyUsed: []string{"patton"},
	}))
	if len(got) == 0 || got[0] != "patton" {
		t.Fatalf("strong evidence should survive the rotation penalty, got %v", got)
	}
}

func TestNeverReturnsNothing(t *testing.T) {
	t.Parallel()

	for _, question := range []string{"", "hm", "?????", "xyzzy plugh"} {
		sel := service.Select(registry(t), service.SelectInput{Question: question, Max: 2})
		if len(sel.Lenses) == 0 {
			t.Fatalf("question %q produced no lens at all", question)
		}
	}
}

func TestSelectionIsDeterministic(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	in := service.SelectInput{Question: "should I ship this or keep polishing it", Max: 3}
	first := ids(service.Select(reg, in))
	for i := 0; i < 5; i++ {
		if got := ids(service.Select(reg, in)); !equal(first, got) {
			t.Fatalf("selection varied between runs: %v then %v", first, got)
		}
	}
}

func TestDefaultsSeedButKeywordsCanOverride(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	// A seeded default with no keyword support still gets picked.
	sel := service.Select(reg, service.SelectInput{
		Question: "nothing in particular",
		Defaults: []string{"giap"},
		Max:      1,
	})
	if got := ids(sel); len(got) == 0 || got[0] != "giap" {
		t.Fatalf("seeded default not used: %v", got)
	}

	// A strong keyword match beats a bare seed.
	sel = service.Select(reg, service.SelectInput{
		Question: "everything is urgent, too many meetings, I'm the bottleneck and need to delegate",
		Defaults: []string{"giap"},
		Max:      1,
	})
	if got := ids(sel); len(got) == 0 || got[0] == "giap" {
		t.Fatalf("keyword evidence should beat a bare default, got %v", got)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Depth buys the argument between lenses. Filling the extra slots from a
// complementary family is what makes it an argument rather than agreement.
func TestBackfillPicksOpposingFamilies(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	sel := service.Select(reg, service.SelectInput{
		Question: "should I ship this or keep working on it",
		Max:      3,
	})

	if len(sel.Lenses) != 3 {
		t.Fatalf("expected 3 lenses at max 3, got %d: %v", len(sel.Lenses), ids(sel))
	}

	seen := map[string]int{}
	for _, lens := range sel.Lenses {
		seen[string(lens.Family)]++
	}
	if len(seen) != 3 {
		t.Fatalf("expected three distinct families, got %v for %v", seen, ids(sel))
	}
}

func TestBackfillNeverDuplicates(t *testing.T) {
	t.Parallel()

	sel := service.Select(registry(t), service.SelectInput{Question: "hm", Max: 3})

	seen := map[string]bool{}
	for _, lens := range sel.Lenses {
		if seen[lens.ID] {
			t.Fatalf("duplicate lens in selection: %v", ids(sel))
		}
		seen[lens.ID] = true
	}
}

func TestBackfillIsDeterministic(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	in := service.SelectInput{Question: "should I ship this", Max: 3}
	first := ids(service.Select(reg, in))
	for i := 0; i < 5; i++ {
		if got := ids(service.Select(reg, in)); !equal(first, got) {
			t.Fatalf("backfill varied: %v then %v", first, got)
		}
	}
}

// A lens with declared rivals gets one of them seated beside it, so the answer
// has an actual fork instead of adjacent lenses nodding along.
func TestRivalsAreSeatedTogether(t *testing.T) {
	t.Parallel()

	sel := service.Select(registry(t), service.SelectInput{
		Question: "I want to wait on this and hold off a bit, there is pressure",
		Max:      2,
	})
	got := ids(sel)
	if len(got) != 2 || got[0] != "kutuzov" {
		t.Fatalf("expected kutuzov to lead, got %v", got)
	}
	if got[1] != "patton" && got[1] != "konev" {
		t.Fatalf("expected one of kutuzov's rivals seated beside him, got %v", got)
	}
}

// A rival is seated on the strength of the pairing alone, so a lens with real
// evidence from the question still outranks it.
func TestKeywordEvidenceOutranksARival(t *testing.T) {
	t.Parallel()

	// "should I" and "decide" are Sun Tzu's; nothing here is Patton's or Konev's.
	sel := service.Select(registry(t), service.SelectInput{
		Question: "should I wait on this or hold off, I need to decide",
		Max:      2,
	})
	if got := ids(sel); len(got) != 2 || got[1] != "sun_tzu" {
		t.Fatalf("keyword evidence should beat a bare rival pairing, got %v", got)
	}
}

// With a single slot there is no one to argue with, so no rival is pulled in.
func TestRivalBonusNeedsRoomForTwo(t *testing.T) {
	t.Parallel()

	sel := service.Select(registry(t), service.SelectInput{Question: "should I wait on this", Max: 1})
	if got := ids(sel); len(got) != 1 || got[0] != "kutuzov" {
		t.Fatalf("got %v", got)
	}
}

// A follow-up in the same conversation keeps the generals already answering it,
// even when its wording would route elsewhere on a fresh pick.
func TestSeatedRosterHoldsOnAnOrdinaryFollowUp(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	got := service.Select(reg, service.SelectInput{
		Question: "What would I do first, though? I feel some pressure here.",
		Seated:   []string{"patton", "eisenhower"},
		Max:      2,
	})

	if got.Method != "sticky" {
		t.Fatalf("method = %q, want sticky", got.Method)
	}
	if ids := ids(got); len(ids) != 2 || ids[0] != "patton" || ids[1] != "eisenhower" {
		t.Fatalf("roster changed on an ordinary follow-up: %v", ids)
	}
}

// Strong evidence for a lens off the bench unseats the weakest sitting lens,
// and only that one.
func TestStrongEvidenceUnseatsTheWeakestLens(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	got := service.Select(reg, service.SelectInput{
		Question: "Everything is urgent, I'm overloaded and drowning in meetings",
		Seated:   []string{"kutuzov", "patton"},
		Max:      2,
	})

	if got.Method != "auto" {
		t.Fatalf("method = %q, want auto after a swap", got.Method)
	}
	ids := ids(got)
	if len(ids) != 2 || ids[1] != "eisenhower" || ids[0] != "kutuzov" {
		t.Fatalf("want eisenhower in patton's seat, kutuzov kept: %v", ids)
	}
}

// A mode's preferred lens holds its seat against a challenger with only a
// little more evidence.
func TestModeDefaultHoldsItsSeat(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	got := service.Select(reg, service.SelectInput{
		Question: "Everything is urgent, I'm overloaded and drowning in meetings",
		Seated:   []string{"patton"},
		Defaults: []string{"patton"},
		Max:      1,
	})

	if ids := ids(got); len(ids) != 1 || ids[0] != "patton" {
		t.Fatalf("mode default lost its seat: %v", ids)
	}
}

// Going deeper mid-conversation adds seats around the sitting roster.
func TestSeatedRosterGrowsWithDepth(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	got := ids(service.Select(reg, service.SelectInput{
		Question: "Go on",
		Seated:   []string{"patton"},
		Max:      3,
	}))

	if len(got) != 3 || got[0] != "patton" {
		t.Fatalf("want patton kept plus two more, got %v", got)
	}
}

// A seated id that is a working style, or no longer exists, is not kept.
func TestSeatedIgnoresStylesAndUnknownIds(t *testing.T) {
	t.Parallel()
	reg := registry(t)

	got := ids(service.Select(reg, service.SelectInput{
		Question: "Go on",
		Seated:   []string{"isolator", "nobody", "kutuzov"},
		Max:      1,
	}))

	if len(got) != 1 || got[0] != "kutuzov" {
		t.Fatalf("got %v, want only kutuzov", got)
	}
}
