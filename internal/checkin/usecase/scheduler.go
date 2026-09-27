package usecase

import (
	"context"
	"errors"
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
	now       func() time.Time
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

func (s *Scheduler) Enabled() bool { return s.enabled && len(s.slots) > 0 }

// Tick checks every slot and the idle condition once. Exported so it can be
// driven directly in tests rather than by waiting on a real ticker.
func (s *Scheduler) Tick(ctx context.Context) {
	if !s.Enabled() {
		return
	}

	now := s.now()
	for _, slot := range s.slots {
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

	s.checkIdle(ctx, now)
}

func (s *Scheduler) checkIdle(ctx context.Context, now time.Time) {
	if s.idleAfter <= 0 || !service.ActiveHours(s.slots, now, s.loc) {
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
	if !s.Enabled() {
		return nil
	}
	local := s.now().In(s.loc)

	var next *time.Time
	for _, slot := range s.slots {
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
func (s *Scheduler) Run(ctx context.Context) {
	if !s.Enabled() {
		log.Printf("check-ins: off (set CHECKIN_ENABLED=true and CHECKIN_SLOTS to turn on)")
		return
	}
	log.Printf("check-ins: %d slots, timezone %s", len(s.slots), s.loc)

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
