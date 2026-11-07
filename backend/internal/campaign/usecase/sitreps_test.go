package usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
)

func TestSitrepsSituationReadsTheMapOrNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, orders := newMemRepo(), newMemOrders()
	manage := NewManage(repo, comusecase.NewManage(orders))
	store := newMemSnapshots()
	snaps := NewSnapshots(manage, store, time.UTC, time.Minute)
	clock := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	snaps.now = func() time.Time { return clock }
	manage.now = snaps.now
	staff := NewSitreps(manage, snaps, time.UTC, true)
	staff.now = snaps.now

	// An empty map is no map: the room answers as before.
	brief, err := staff.Situation(ctx)
	if err != nil || brief != "" {
		t.Fatalf("empty map: %q %v", brief, err)
	}
	sitrep, _ := staff.Sitrep(ctx)
	if !sitrep.Empty {
		t.Fatal("the sitrep of an empty map is empty")
	}

	front, _ := manage.CreateFront(ctx, "Work")
	ship, err := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship v2", FrontID: &front.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manage.CreateItem(ctx, CreateInput{Type: domain.TypeUnknown, Text: "Who pays?", FrontID: &front.ID, ObjectiveID: &ship.ID}); err != nil {
		t.Fatal(err)
	}
	// Yesterday's map, for the line to be compared against.
	if _, err := snaps.Take(ctx); err != nil {
		t.Fatal(err)
	}
	clock = clock.AddDate(0, 0, 1)

	brief, err = staff.Situation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Front: Work", "◆ Ship v2", "? Who pays? — fog, no reconnaissance", "(was 0% on 2026-10-13)", "has no reconnaissance"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief lacks %q:\n%s", want, brief)
		}
	}
	sitrep, _ = staff.Sitrep(ctx)
	if sitrep.Since != "2026-10-13" || !sitrep.Fronts[0].HasPrior {
		t.Fatalf("the sitrep compares against yesterday's snapshot: %+v", sitrep)
	}

	// Switched off, the staff says nothing even with a map.
	off := NewSitreps(manage, snaps, time.UTC, false)
	off.now = snaps.now
	if brief, _ := off.Situation(ctx); brief != "" {
		t.Fatalf("CAMPAIGN_STAFF_ENABLED=false should read nothing in, got:\n%s", brief)
	}
}

func TestSnapshotsLastBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMemSnapshots()
	snaps := NewSnapshots(NewManage(newMemRepo(), comusecase.NewManage(newMemOrders())), store, time.UTC, time.Minute)

	if last, err := snaps.LastBefore(ctx, "2026-10-13"); err != nil || last != nil {
		t.Fatalf("no record, nothing before: %v %v", last, err)
	}
	for _, day := range []string{"2026-10-01", "2026-10-11", "2026-10-13"} {
		_ = store.SaveSnapshot(ctx, domain.Snapshot{Day: day, Theatre: domain.Theatre{Day: day}})
	}
	if last, _ := snaps.LastBefore(ctx, "2026-10-13"); last == nil || last.Day != "2026-10-11" {
		t.Fatalf("want the latest day before, got %+v", last)
	}
	if last, _ := snaps.LastBefore(ctx, "2026-10-01"); last != nil {
		t.Fatalf("nothing before the first day: %+v", last)
	}
	if _, err := snaps.LastBefore(ctx, "yesterday"); err == nil {
		t.Fatal("a bad day is refused")
	}
}
