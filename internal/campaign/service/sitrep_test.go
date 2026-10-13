package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

func hasReading(rs []domain.Reading, kind domain.ReadingKind) bool {
	for _, r := range rs {
		if r.Kind == kind {
			return true
		}
	}
	return false
}

func TestReadIsEmptyOnAnEmptyMap(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	s := Read(nil, nil, nil, nil, now)
	if !s.Empty || len(s.Fronts) != 0 || len(s.Readings) != 0 {
		t.Fatalf("an empty map reads as empty: %+v", s)
	}
	if Brief(s) != "" {
		t.Fatal("an empty map puts nothing in the prompt")
	}
	// A front with nothing on it is still a map.
	s = Read([]domain.Front{{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}}, nil, nil, nil, now)
	if s.Empty || !strings.Contains(Brief(s), "Front: Work") {
		t.Fatalf("a front alone is a map: %+v", s)
	}
}

func TestReadLaysOutOrdersMovementAndReadings(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	health := domain.Front{ID: uuid.New(), Name: "Health", Status: domain.FrontActive}

	wasDue := now.Add(-3 * 24 * time.Hour)
	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive, DueAt: &wasDue}
	fortress := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &work.ID, ObjectiveID: &ship.ID,
		Text: "No tests", Status: domain.StatusActive, Strength: 3, Kind: domain.KindTooBig}
	unwanted := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &work.ID, ObjectiveID: &ship.ID,
		Text: "Nobody asked for it", Status: domain.StatusActive, Strength: 1, StrengthConfirmed: true, Kind: domain.KindUnwanted}
	fog := domain.Item{ID: uuid.New(), Type: domain.TypeUnknown, FrontID: &work.ID, ObjectiveID: &ship.ID, Text: "Who pays?", Status: domain.StatusActive}
	scouted := domain.Item{ID: uuid.New(), Type: domain.TypeUnknown, FrontID: &work.ID, ObjectiveID: &ship.ID, Text: "Which cloud?", Status: domain.StatusActive}
	earlier := now.Add(-2 * time.Hour)
	cleared := domain.Item{ID: uuid.New(), Type: domain.TypeObstacle, FrontID: &work.ID, ObjectiveID: &ship.ID,
		Text: "No CI", Status: domain.StatusResolved, Strength: 1, ResolvedAt: &earlier}
	longAgo := now.Add(-5 * 24 * time.Hour)
	oldTaken := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Hire", Status: domain.StatusResolved, ResolvedAt: &longAgo}
	run := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &health.ID, Text: "Run a 10k", Status: domain.StatusActive}
	proposal := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &health.ID, Text: "Maybe", Status: domain.StatusProposed}

	today := time.Date(2026, 10, 13, 23, 59, 0, 0, time.UTC)
	yesterday := today.Add(-24 * time.Hour)
	orders := []comdomain.Commitment{
		{ID: uuid.New(), Text: "Write the tests", Status: comdomain.StatusOpen, TargetID: &unwanted.ID, DueAt: &today},
		{ID: uuid.New(), Text: "Ask Sam", Status: comdomain.StatusOpen, Kind: comdomain.KindRecon, TargetID: &scouted.ID, DueAt: &yesterday},
		{ID: uuid.New(), Text: "Draft the announcement", Status: comdomain.StatusOpen, TargetID: &ship.ID},
		{ID: uuid.New(), Text: "Set up CI", Status: comdomain.StatusDone, TargetID: &ship.ID, ResolvedAt: &earlier},
		{ID: uuid.New(), Text: "Not agreed", Status: comdomain.StatusProposed, TargetID: &ship.ID},
	}

	prior := &domain.Theatre{Day: "2026-10-12", Fronts: []domain.TheatreFront{
		{ID: work.ID, Name: "Work", Line: 0.10},
		{ID: health.ID, Name: "Health", Line: 0},
	}}

	s := Read([]domain.Front{work, health}, []domain.Item{ship, fortress, unwanted, fog, scouted, cleared, oldTaken, run, proposal}, orders, prior, now)
	if s.Empty || len(s.Fronts) != 2 || s.Since != "2026-10-12" {
		t.Fatalf("sitrep shape: %+v", s)
	}

	w := s.Fronts[0]
	if w.Name != "Work" || len(w.Objectives) != 2 || !w.HasPrior || w.PriorLine != 0.10 {
		t.Fatalf("Work: %+v", w)
	}
	if w.OrdersOpen != 3 || w.OrdersDue != 2 {
		t.Fatalf("Work orders: open %d due %d (proposed and done ones do not count)", w.OrdersOpen, w.OrdersDue)
	}
	row := w.Objectives[0]
	if row.Text != "Ship v2" || !row.Overdue || len(row.Opposition) != 3 || len(row.Fog) != 2 {
		t.Fatalf("Ship v2 row: %+v", row)
	}
	if len(row.Orders) != 2 || row.Orders[0].Text != "Draft the announcement" || !row.Orders[1].Done {
		t.Fatalf("objective orders: %+v", row.Orders)
	}
	var sam, who domain.SitrepPost
	for _, p := range row.Fog {
		switch p.Text {
		case "Which cloud?":
			sam = p
		case "Who pays?":
			who = p
		}
	}
	if !sam.Scouted || len(sam.Orders) != 1 || !sam.Orders[0].Overdue || !sam.Orders[0].Recon {
		t.Fatalf("a recon order marks fog scouted and overdue: %+v", sam)
	}
	if who.Scouted {
		t.Fatal("fog with no recon order is unscouted")
	}
	for _, p := range row.Opposition {
		if p.Text == "Nobody asked for it" && (len(p.Orders) != 1 || !p.Orders[0].DueToday) {
			t.Fatalf("an order due today: %+v", p.Orders)
		}
	}

	h := s.Fronts[1]
	if h.Name != "Health" || !h.Quiet || len(h.Objectives) != 1 {
		t.Fatalf("Health has a live objective, no orders and no movement: quiet. %+v", h)
	}

	m := s.Moved
	if len(m.Cleared) != 1 || m.Cleared[0] != "No CI" || len(m.OrdersDone) != 1 || m.OrdersDone[0] != "Set up CI" {
		t.Fatalf("moved in the last day: %+v", m)
	}
	if len(m.Taken) != 0 {
		t.Fatal("an objective taken five days ago is not today's movement")
	}
	if len(m.Lines) != 1 || m.Lines[0].Front != "Work" || m.Lines[0].From != 0.10 {
		t.Fatalf("the Work line moved against the prior snapshot: %+v", m.Lines)
	}

	for _, kind := range []domain.ReadingKind{
		domain.ReadingOverdue, domain.ReadingQuiet, domain.ReadingUnscouted,
		domain.ReadingFortress, domain.ReadingUnwanted, domain.ReadingEstimated, domain.ReadingAdvanced,
	} {
		if !hasReading(s.Readings, kind) {
			t.Fatalf("missing the %s reading in %+v", kind, s.Readings)
		}
	}
	if hasReading(s.Readings, domain.ReadingOverextended) {
		t.Fatal("three orders open with the line moving is not overextension")
	}
	if s.Readings[0].Kind != domain.ReadingOverdue {
		t.Fatalf("what is late comes first: %+v", s.Readings[0])
	}

	brief := Brief(s)
	for _, want := range []string{
		"Front: Work — line", "(was 10% on 2026-10-12)", "3 orders open (2 due today or overdue)",
		"◆ Ship v2 — active, overdue, was due 2026-10-10", "▲ No tests — too big to hold, fortress (est)",
		"? Who pays? — fog, no reconnaissance", "? Which cloud? — fog, reconnaissance sent",
		"orders: Ask Sam (recon, overdue, was due 2026-10-12)", "orders: Write the tests (due today)",
		"done: 1 order", "Front: Health — line 0%", "quiet",
		"Moved in the last 24 hours: cleared \"No CI\"; orders done \"Set up CI\"; Work line 10% →",
		"Staff readings", "is overdue", "Health has gone quiet",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "Maybe") || strings.Contains(brief, "Not agreed") {
		t.Fatalf("proposals stay off the brief:\n%s", brief)
	}
}

func TestReadOverextendedAndNoPrior(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	work := domain.Front{ID: uuid.New(), Name: "Work", Status: domain.FrontActive}
	ship := domain.Item{ID: uuid.New(), Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship v2", Status: domain.StatusActive}
	var orders []comdomain.Commitment
	for _, text := range []string{"a", "b", "c", "d"} {
		orders = append(orders, comdomain.Commitment{ID: uuid.New(), Text: text, Status: comdomain.StatusOpen, TargetID: &ship.ID})
	}

	s := Read([]domain.Front{work}, []domain.Item{ship}, orders, nil, now)
	if s.Since != "" || s.Fronts[0].HasPrior || s.Fronts[0].Quiet {
		t.Fatalf("with no record there is nothing to be quiet against: %+v", s.Fronts[0])
	}
	if !hasReading(s.Readings, domain.ReadingOverextended) {
		t.Fatalf("four orders open and nothing moved is overextension: %+v", s.Readings)
	}
	if hasReading(s.Readings, domain.ReadingAdvanced) {
		t.Fatal("an objective with no ground held is not the closest to taken")
	}
	brief := Brief(s)
	if !strings.Contains(brief, "Work is overextended: 4 orders open and nothing has moved.") {
		t.Fatalf("brief:\n%s", brief)
	}
	if strings.Contains(brief, "(was") {
		t.Fatal("no prior line to compare against")
	}
	if !strings.Contains(brief, "Moved in the last 24 hours: nothing.") {
		t.Fatalf("brief:\n%s", brief)
	}
}

func TestParseRedTeamReadsAttacksAndCover(t *testing.T) {
	t.Parallel()
	answer := `**Enemy commander**
Manstein. Your map reads as two bad options.

**How I hold**
- ▲ No tests holds because you never cut it down.

**Where the plan breaks**
1. Your order "Write the tests" hits ▲ No tests head on; I let you start, and the early sign is a third day with the suite half green. (too_big)
- Your objective ◆ Ship v2 has no date, so I wait; the sign is the week ending with the announcement still a draft (undefined)
* Sam never answers, and you never chase him; the sign is "waiting on Sam" in your notes twice (waiting).
- A fourth attack the template did not ask for (fear)

**Your fog, my cover**
- Who pays for hosting once the beta is open? What you would find: a number you can afford.
- Does the client actually read the changelog?
- This is not a question, so it is not fog

**I concede**
The CI is real.

**To beat me**
- Cut the tests to the three that gate the release, by Thursday; fallback: ship with the two that pass.
`
	intel := ParseRedTeam(answer)
	if len(intel.Attacks) != 3 {
		t.Fatalf("three attacks, the fourth is padding: %+v", intel.Attacks)
	}
	if intel.Attacks[0].Kind != domain.KindTooBig || strings.Contains(intel.Attacks[0].Text, "(too_big)") || !strings.HasPrefix(intel.Attacks[0].Text, "Your order") {
		t.Fatalf("first attack: %+v", intel.Attacks[0])
	}
	if intel.Attacks[1].Kind != domain.KindUndefined || intel.Attacks[2].Kind != domain.KindWaiting || strings.HasSuffix(intel.Attacks[2].Text, ".") {
		t.Fatalf("kinds and trailing stops: %+v", intel.Attacks[1:])
	}
	if len(intel.Cover) != 2 || intel.Cover[0] != "Who pays for hosting once the beta is open?" || intel.Cover[1] != "Does the client actually read the changelog?" {
		t.Fatalf("cover is the questions alone: %+v", intel.Cover)
	}

	long := "**Where the plan breaks**\n- " + strings.Repeat("word ", 30) + "first sentence ends here. And a second sentence that runs on past the limit of what one item should be, well beyond it. (fear)"
	cut := ParseRedTeam(long)
	if len(cut.Attacks) != 1 || len([]rune(cut.Attacks[0].Text)) > 200 || !strings.HasSuffix(cut.Attacks[0].Text, "first sentence ends here") {
		t.Fatalf("a long attack is cut at a sentence end: %q", cut.Attacks[0].Text)
	}

	if got := ParseRedTeam("Just prose with no sections."); len(got.Attacks)+len(got.Cover) != 0 {
		t.Fatalf("nothing parses to nothing: %+v", got)
	}
}
