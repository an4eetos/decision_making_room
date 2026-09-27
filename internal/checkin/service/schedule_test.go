package service

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

func TestParseSlots(t *testing.T) {
	t.Parallel()

	slots, err := ParseSlots("morning@08:00, midday@13:30,evening@21:00")
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 3 {
		t.Fatalf("got %d slots", len(slots))
	}
	if slots[1].Hour != 13 || slots[1].Minute != 30 {
		t.Fatalf("midday parsed as %d:%d", slots[1].Hour, slots[1].Minute)
	}
	want := []string{"day_plan", "block_start", "debrief"}
	for i, s := range slots {
		if s.ModeID != want[i] {
			t.Fatalf("slot %s mode = %q, want %q", s.Name, s.ModeID, want[i])
		}
	}
}

// Off by default. A tool that starts messaging you unasked gets uninstalled.
func TestParseSlotsEmptyMeansNone(t *testing.T) {
	t.Parallel()

	slots, err := ParseSlots("  ")
	if err != nil || slots != nil {
		t.Fatalf("got %v, %v", slots, err)
	}
}

func TestParseSlotsRejectsGarbage(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"morning", "morning@8", "morning@25:00", "morning@08:61", "@08:00", "a@08:00,a@09:00"} {
		if _, err := ParseSlots(raw); err == nil {
			t.Errorf("%q should be rejected", raw)
		}
	}
}

func TestCustomSlotNameFallsBackToTimeOfDay(t *testing.T) {
	t.Parallel()

	slots, _ := ParseSlots("standup@09:30,wind_down@22:00")
	if slots[0].ModeID != "day_plan" || slots[1].ModeID != "debrief" {
		t.Fatalf("got %s, %s", slots[0].ModeID, slots[1].ModeID)
	}
}

func TestDueFiresAfterSlotTimeWithinGrace(t *testing.T) {
	t.Parallel()

	loc := mustLoc(t, "Asia/Almaty")
	slot := Slot{Name: "morning", Hour: 8}

	before := time.Date(2026, 9, 26, 7, 59, 0, 0, loc)
	if _, due := Due(slot, before, loc, 90*time.Minute); due {
		t.Fatal("fired before its time")
	}

	at := time.Date(2026, 9, 26, 8, 0, 0, 0, loc)
	date, due := Due(slot, at, loc, 90*time.Minute)
	if !due || date.Format("2006-01-02") != "2026-09-26" {
		t.Fatalf("should fire at 08:00 for the 26th, got %v %v", due, date)
	}
}

// A laptop asleep through 08:00 that wakes at 08:45 should still get its
// morning check-in. A ticker that only fired on the exact minute would skip it.
func TestDueCatchesUpAfterSleep(t *testing.T) {
	t.Parallel()

	loc := mustLoc(t, "Asia/Almaty")
	woke := time.Date(2026, 9, 26, 8, 45, 0, 0, loc)

	if _, due := Due(Slot{Hour: 8}, woke, loc, 90*time.Minute); !due {
		t.Fatal("a slot missed by 45 minutes should still fire on wake")
	}
}

// And one missed by hours should not: a morning plan at 23:00 is useless.
func TestDueRespectsGrace(t *testing.T) {
	t.Parallel()

	loc := mustLoc(t, "Asia/Almaty")
	late := time.Date(2026, 9, 26, 23, 0, 0, 0, loc)

	if _, due := Due(Slot{Hour: 8}, late, loc, 90*time.Minute); due {
		t.Fatal("fired fifteen hours late")
	}
}

// The day is the user's day, not the server's. At 01:00 in Almaty it is still
// the previous evening in UTC, and the wrong date would double-fire or skip.
func TestDueUsesLocalCalendarDay(t *testing.T) {
	t.Parallel()

	loc := mustLoc(t, "Asia/Almaty") // UTC+5
	// 00:30 local on the 27th is 19:30 UTC on the 26th.
	now := time.Date(2026, 9, 26, 19, 30, 0, 0, time.UTC)

	date, due := Due(Slot{Hour: 0, Minute: 15}, now, loc, time.Hour)
	if !due {
		t.Fatal("expected due")
	}
	if got := date.Format("2006-01-02"); got != "2026-09-27" {
		t.Fatalf("local date = %s, want the 27th", got)
	}
}

func TestActiveHoursSpansFirstToLastSlot(t *testing.T) {
	t.Parallel()

	loc := mustLoc(t, "Asia/Almaty")
	slots, _ := ParseSlots("morning@08:00,evening@21:00")

	cases := map[int]bool{7: false, 8: true, 14: true, 21: true, 22: false, 3: false}
	for hour, want := range cases {
		now := time.Date(2026, 9, 26, hour, 0, 0, 0, loc)
		if got := ActiveHours(slots, now, loc); got != want {
			t.Errorf("%02d:00 active = %v, want %v", hour, got, want)
		}
	}
}

func TestNoSlotsMeansNoActiveHours(t *testing.T) {
	t.Parallel()

	if ActiveHours(nil, time.Now(), time.UTC) {
		t.Fatal("with no slots there is no window for nudges")
	}
}
