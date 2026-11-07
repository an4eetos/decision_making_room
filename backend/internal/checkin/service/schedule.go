// Package service holds the scheduling rules, kept pure so the edge cases —
// laptop sleep, restarts, midnight, a slot missed by hours — are testable
// without a clock or a database.
package service

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Slot is a named time of day, like morning@08:00.
type Slot struct {
	Name   string
	Hour   int
	Minute int
	// ModeID shapes the check-in: planning in the morning, re-focusing midday,
	// a debrief in the evening.
	ModeID string
}

// defaultModes maps the conventional slot names onto modes. A custom slot name
// falls back to the time of day.
var defaultModes = map[string]string{
	"morning": "day_plan",
	"midday":  "block_start",
	"noon":    "block_start",
	"evening": "debrief",
	"night":   "debrief",
}

// ParseSlots reads "morning@08:00,midday@13:00,evening@21:00". An empty string
// means no scheduled check-ins, which is the default: a tool that starts
// messaging you unasked is one people uninstall.
func ParseSlots(raw string) ([]Slot, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	seen := map[string]bool{}
	var slots []Slot

	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		name, at, ok := strings.Cut(part, "@")
		name = strings.ToLower(strings.TrimSpace(name))
		if !ok || name == "" {
			return nil, fmt.Errorf("check-in slot %q: want name@HH:MM", part)
		}
		if seen[name] {
			return nil, fmt.Errorf("check-in slot %q appears twice", name)
		}
		seen[name] = true

		hh, mm, ok := strings.Cut(strings.TrimSpace(at), ":")
		hour, errH := strconv.Atoi(hh)
		minute, errM := strconv.Atoi(mm)
		if !ok || errH != nil || errM != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
			return nil, fmt.Errorf("check-in slot %q: time must be HH:MM", part)
		}

		slots = append(slots, Slot{Name: name, Hour: hour, Minute: minute, ModeID: modeFor(name, hour)})
	}

	return slots, nil
}

func modeFor(name string, hour int) string {
	if mode, ok := defaultModes[name]; ok {
		return mode
	}
	return ModeForHour(hour)
}

// ModeForHour picks a check-in shape from the time of day, for slots with
// unconventional names and for "check in now".
func ModeForHour(hour int) string {
	switch {
	case hour < 11:
		return "day_plan"
	case hour < 17:
		return "block_start"
	default:
		return "debrief"
	}
}

// Due reports whether a slot should fire now, and for which local date.
//
// It fires when the slot's time today has passed and is still within grace. The
// grace window is what makes a slept-through slot fire on wake — a ticker that
// only fired at the exact minute would silently skip it — while stopping an
// 08:00 check-in from arriving at 23:00, when it would be useless.
//
// Whether it has already fired today is the caller's question, answered by the
// database rather than memory, so a restart cannot make it fire twice.
func Due(slot Slot, now time.Time, loc *time.Location, grace time.Duration) (time.Time, bool) {
	local := now.In(loc)
	at := time.Date(local.Year(), local.Month(), local.Day(), slot.Hour, slot.Minute, 0, 0, loc)

	if local.Before(at) {
		return time.Time{}, false
	}
	if grace > 0 && local.Sub(at) > grace {
		return time.Time{}, false
	}

	return LocalDate(local), true
}

// LocalDate truncates to the calendar day. Stored as a DATE, so the location is
// irrelevant once it is written — what matters is that the day is computed in
// the user's timezone, not the server's.
func LocalDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ActiveHours reports whether now falls between the first and last slot of the
// day. Idle nudges only fire inside that window: "what happened since this
// morning?" at 3am is noise, and with no slots there is no window at all.
func ActiveHours(slots []Slot, now time.Time, loc *time.Location) bool {
	if len(slots) == 0 {
		return false
	}

	local := now.In(loc)
	minutes := local.Hour()*60 + local.Minute()

	first, last := 24*60, -1
	for _, s := range slots {
		m := s.Hour*60 + s.Minute
		if m < first {
			first = m
		}
		if m > last {
			last = m
		}
	}
	return minutes >= first && minutes <= last
}
