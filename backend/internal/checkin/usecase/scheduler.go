package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/an4eetos/decision-room/internal/checkin/port"
	"github.com/an4eetos/decision-room/internal/checkin/service"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

// Scheduler fires check-ins on a one-minute ticker.
//
// A ticker plus a database uniqueness check, rather than a cron library, because
// cron holds its schedule in memory and silently skips a slot the laptop slept
// through. Here every tick asks "is a slot due that has not fired today?", so on
// wake it notices the missed one and fires it — within the grace window.
type Scheduler struct {
	gen       *Generate
	chats     memport.ChatRepository
	slots     []service.Slot
	loc       *time.Location
	grace     time.Duration
	idleAfter time.Duration
	enabled   bool
	// live, when set, replaces enabled and slots at every tick: both are
	// settings a client can change while the room runs.
	live func() (enabled bool, slots []service.Slot)
	now  func() time.Time
}

type SchedulerConfig struct {
	Enabled   bool
	Slots     []service.Slot
	Location  *time.Location
	Grace     time.Duration
	IdleAfter time.Duration
}

func NewScheduler(gen *Generate, chats memport.ChatRepository, cfg SchedulerConfig) *Scheduler {
	loc := cfg.Location
	if loc == nil {
		loc = time.Local
	}
	return &Scheduler{
		gen: gen, chats: chats, slots: cfg.Slots, loc: loc,
		grace: cfg.Grace, idleAfter: cfg.IdleAfter,
		enabled: cfg.Enabled, now: time.Now,
	}
}

// WithLive reads whether check-ins are on, and their slots, at every tick.
func (s *Scheduler) WithLive(fn func() (bool, []service.Slot)) *Scheduler {
	s.live = fn
	return s
}

// state is whether check-ins are on and their slots, now.
func (s *Scheduler) state() (bool, []service.Slot) {
	if s.live != nil {
		return s.live()
	}
	return s.enabled, s.slots
}

func (s *Scheduler) Enabled() bool {
	on, slots := s.state()
	return on && len(slots) > 0
}

// Schedule is what a client needs to set its own alarms: the slots and the
// timezone they fire in. A phone that is off at 08:00 reads the check-in when
// it is on, the same rule a laptop that slept already follows; nothing is
// pushed, the text waits on the server.
func (s *Scheduler) Schedule() ScheduleInfo {
	_, current := s.state()
	slots := make([]ScheduledSlot, 0, len(current))
	for _, slot := range current {
		slots = append(slots, ScheduledSlot{
			Name: slot.Name, Time: fmt.Sprintf("%02d:%02d", slot.Hour, slot.Minute), ModeID: slot.ModeID,
		})
	}
	return ScheduleInfo{Enabled: s.Enabled(), Timezone: s.loc.String(), Slots: slots}
}

// ScheduleInfo is the check-in timetable as served to clients.
type ScheduleInfo struct {
	Enabled  bool            `json:"enabled"`
	Timezone string          `json:"timezone"`
	Slots    []ScheduledSlot `json:"slots"`
}

// ScheduledSlot is one slot, its time as "HH:MM" in the schedule's timezone.
type ScheduledSlot struct {
	Name   string `json:"name"`
	Time   string `json:"time"`
	ModeID string `json:"mode_id"`
}

// Tick checks every slot and the idle condition once. Exported so it can be
// driven directly in tests rather than by waiting on a real ticker.
func (s *Scheduler) Tick(ctx context.Context) {
	on, slots := s.state()
	if !on || len(slots) == 0 {
		return
	}

	now := s.now()
	for _, slot := range slots {
		date, due := service.Due(slot, now, s.loc, s.grace)
		if !due {
			continue
		}
		if _, err := s.gen.Scheduled(ctx, slot, date); err != nil && !errors.Is(err, port.ErrAlreadyFired) {
			// Logged, not retried immediately: the next tick is a minute away
			// and still inside the grace window.
			log.Printf("check-in: %s failed: %v", slot.Name, err)
		}
	}

	s.checkIdle(ctx, now, slots)
}

func (s *Scheduler) checkIdle(ctx context.Context, now time.Time, slots []service.Slot) {
	if s.idleAfter <= 0 || !service.ActiveHours(slots, now, s.loc) {
		return
	}

	last, err := s.chats.LastUserMessageAt(ctx)
	if err != nil {
		log.Printf("check-in: idle check failed: %v", err)
		return
	}
	// Someone who has never written anything is new, not idle.
	if last == nil || now.Sub(*last) < s.idleAfter {
		return
	}

	if _, err := s.gen.Idle(ctx, *last); err != nil && !errors.Is(err, port.ErrAlreadyFired) {
		log.Printf("check-in: idle nudge failed: %v", err)
	}
}

// Next returns when the next scheduled check-in is due, or nil when they are
// off. Today's later slot if there is one, otherwise tomorrow's first.
func (s *Scheduler) Next() *time.Time {
	on, slots := s.state()
	if !on || len(slots) == 0 {
		return nil
	}
	local := s.now().In(s.loc)

	var next *time.Time
	for _, slot := range slots {
		at := time.Date(local.Year(), local.Month(), local.Day(), slot.Hour, slot.Minute, 0, 0, s.loc)
		if !at.After(local) {
			at = at.AddDate(0, 0, 1)
		}
		if next == nil || at.Before(*next) {
			t := at
			next = &t
		}
	}
	return next
}

// Run ticks until the context ends. It ticks once immediately, so starting the
// app after a slot's time catches up without waiting a minute.
// It keeps ticking while check-ins are off, because they can be turned on in
// Settings; an off tick costs nothing.
func (s *Scheduler) Run(ctx context.Context) {
	if on, slots := s.state(); on && len(slots) > 0 {
		log.Printf("check-ins: %d slots, timezone %s", len(slots), s.loc)
	} else {
		log.Printf("check-ins: off (turn them on in Settings, or set CHECKIN_ENABLED=true)")
	}

	s.Tick(ctx)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// OnStale implements the commitments module's StaleListener, so a stale nudge
// fires at the moment of the transition rather than on a daily reread.
func (s *Scheduler) OnStale(ctx context.Context, items []comdomain.Commitment) {
	if !s.Enabled() {
		return
	}
	if _, err := s.gen.Stale(ctx, items); err != nil && !errors.Is(err, port.ErrAlreadyFired) {
		log.Printf("check-in: stale nudge failed: %v", err)
	}
}
