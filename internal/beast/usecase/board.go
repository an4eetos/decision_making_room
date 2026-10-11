// Package usecase runs beast mode's board: clocks on orders, re-armed when
// they run out, cleared only by a report.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/beast/domain"
	"github.com/an4eetos/decision-room/internal/beast/port"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

var (
	ErrBadTransition = errors.New("a resolved task cannot be changed")
	ErrEmptyText     = errors.New("task text is required")
)

// maxLoops bounds the loops in a brief. A crisis has one to three fronts; a
// longer list is the backlog, and reading it out is not what the alarm is for.
const maxLoops = 5

type Board struct {
	repo  port.Repository
	loops port.Loops
	now   func() time.Time
}

// NewBoard takes loops as optional: without them the board still keeps time,
// it just cannot see or close open loops.
func NewBoard(repo port.Repository, loops port.Loops) *Board {
	return &Board{repo: repo, loops: loops, now: func() time.Time { return time.Now().UTC() }}
}

// List is the session's whole board in number order.
func (b *Board) List(ctx context.Context, sessionID uuid.UUID) ([]domain.Task, error) {
	return b.repo.ListBySession(ctx, sessionID)
}

// Add puts a task on the board by hand.
func (b *Board) Add(ctx context.Context, sessionID uuid.UUID, text string, minutes int) (domain.Task, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.Task{}, ErrEmptyText
	}
	minutes = domain.ClampMinutes(minutes)
	return b.repo.Create(ctx, domain.Task{
		SessionID:  sessionID,
		Text:       text,
		Minutes:    minutes,
		DeadlineAt: b.now().Add(time.Duration(minutes) * time.Minute),
		Status:     domain.StatusOpen,
	})
}

// EditInput changes an open task. Minutes starts a fresh clock of that length
// from now; Extend moves the current deadline by that many minutes, either way.
type EditInput struct {
	Text    *string
	Minutes *int
	Extend  int
}

func (b *Board) Edit(ctx context.Context, id uuid.UUID, in EditInput) (domain.Task, error) {
	t, err := b.repo.Get(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if t.Status.Resolved() {
		return domain.Task{}, ErrBadTransition
	}

	now := b.now()
	if in.Text != nil {
		text := strings.TrimSpace(*in.Text)
		if text == "" {
			return domain.Task{}, ErrEmptyText
		}
		t.Text = text
	}
	if in.Minutes != nil {
		t.Minutes = domain.ClampMinutes(*in.Minutes)
		t.DeadlineAt = now.Add(time.Duration(t.Minutes) * time.Minute)
	}
	if in.Extend != 0 {
		t.DeadlineAt = t.DeadlineAt.Add(time.Duration(in.Extend) * time.Minute)
		// Pulling a clock into the past would fire an eruption you set off
		// yourself; a minute from now is as tight as it goes.
		if floor := now.Add(time.Minute); t.DeadlineAt.Before(floor) {
			t.DeadlineAt = floor
		}
	}
	return b.repo.Update(ctx, t)
}

// Complete reports one task done, closing its open loop if it has one.
func (b *Board) Complete(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	t, err := b.repo.Get(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	return b.complete(ctx, t)
}

// Abandon strikes a task off without doing it.
func (b *Board) Abandon(ctx context.Context, id uuid.UUID) (domain.Task, error) {
	t, err := b.repo.Get(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	if t.Status.Resolved() {
		return domain.Task{}, ErrBadTransition
	}
	t.Status = domain.StatusAbandoned
	return b.repo.Update(ctx, t)
}

func (b *Board) complete(ctx context.Context, t domain.Task) (domain.Task, error) {
	switch t.Status {
	case domain.StatusDone:
		return t, nil
	case domain.StatusAbandoned:
		return domain.Task{}, ErrBadTransition
	}

	t.Status = domain.StatusDone
	done, err := b.repo.Update(ctx, t)
	if err != nil {
		return domain.Task{}, err
	}
	if done.CommitmentID != nil && b.loops != nil {
		// The task is done either way; a loop that could not be closed is
		// still there to tick by hand.
		if err := b.loops.Done(ctx, *done.CommitmentID); err != nil {
			log.Printf("beast: close loop %s: %v", done.CommitmentID, err)
		}
	}
	return done, nil
}

// The methods below are the board as the chat sees it (memport.BeastBoard).

func (b *Board) Tasks(ctx context.Context, sessionID uuid.UUID) ([]memport.BeastTask, error) {
	tasks, err := b.repo.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return toBrief(tasks), nil
}

// DueLoops is the live loops due by the end of today, local time: the crisis.
func (b *Board) DueLoops(ctx context.Context, now time.Time) ([]memport.BeastLoop, error) {
	if b.loops == nil {
		return nil, nil
	}
	local := now.In(time.Local)
	endOfDay := time.Date(local.Year(), local.Month(), local.Day(), 23, 59, 59, 0, time.Local)
	loops, err := b.loops.DueBy(ctx, endOfDay)
	if err != nil {
		return nil, fmt.Errorf("due loops: %w", err)
	}
	if len(loops) > maxLoops {
		loops = loops[:maxLoops]
	}

	out := make([]memport.BeastLoop, 0, len(loops))
	for i, l := range loops {
		out = append(out, memport.BeastLoop{ID: l.ID, Number: i + 1, Text: l.Text, DueAt: l.DueAt})
	}
	return out, nil
}

// Expire re-arms every open task past its deadline. Rearm is conditional on
// the task still being late, so two tabs racing the same clock re-arm it once.
func (b *Board) Expire(ctx context.Context, sessionID uuid.UUID, now time.Time) ([]memport.BeastTask, error) {
	tasks, err := b.repo.ListBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	var expired []domain.Task
	for _, t := range tasks {
		if !t.Overdue(now) {
			continue
		}
		deadline := now.Add(domain.RearmWindow(t.Minutes, t.Expiries+1))
		rearmed, ok, err := b.repo.Rearm(ctx, t.ID, now, deadline)
		if err != nil {
			return nil, err
		}
		if ok {
			expired = append(expired, rearmed)
		}
	}
	return toBrief(expired), nil
}

// Report marks the named tasks done and returns every named task of the
// session that is now done, including ones a moment-earlier click already
// closed: the report turn is about them either way.
func (b *Board) Report(ctx context.Context, sessionID uuid.UUID, taskIDs []uuid.UUID) ([]memport.BeastTask, error) {
	var out []domain.Task
	for _, id := range taskIDs {
		t, err := b.repo.Get(ctx, id)
		if errors.Is(err, port.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if t.SessionID != sessionID {
			continue
		}
		done, err := b.complete(ctx, t)
		if errors.Is(err, ErrBadTransition) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, done)
	}
	return toBrief(out), nil
}

// Apply records the generals' turn. An order already on the board as an open
// task is not added twice, whatever the model was told.
func (b *Board) Apply(ctx context.Context, sessionID, messageID uuid.UUID, orders []memport.BeastOrder, done []int, loops []memport.BeastLoop) error {
	tasks, err := b.repo.ListBySession(ctx, sessionID)
	if err != nil {
		return err
	}

	bySeq := make(map[int]domain.Task, len(tasks))
	open := make(map[string]bool)
	for _, t := range tasks {
		bySeq[t.Seq] = t
		if t.Status == domain.StatusOpen {
			open[strings.ToLower(t.Text)] = true
		}
	}

	for _, n := range done {
		t, ok := bySeq[n]
		if !ok || t.Status != domain.StatusOpen {
			continue
		}
		if _, err := b.complete(ctx, t); err != nil {
			return err
		}
	}

	now := b.now()
	for _, o := range orders {
		key := strings.ToLower(strings.TrimSpace(o.Text))
		if key == "" || open[key] {
			continue
		}
		open[key] = true

		minutes := domain.ClampMinutes(o.Minutes)
		task := domain.Task{
			SessionID:  sessionID,
			MessageID:  &messageID,
			Text:       strings.TrimSpace(o.Text),
			Minutes:    minutes,
			DeadlineAt: now.Add(time.Duration(minutes) * time.Minute),
			Status:     domain.StatusOpen,
		}
		for _, l := range loops {
			if l.Number == o.Loop {
				id := l.ID
				task.CommitmentID = &id
			}
		}
		if _, err := b.repo.Create(ctx, task); err != nil {
			return err
		}
	}
	return nil
}

// StandDown abandons whatever is still open.
func (b *Board) StandDown(ctx context.Context, sessionID uuid.UUID) error {
	tasks, err := b.repo.ListBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.Status != domain.StatusOpen {
			continue
		}
		t.Status = domain.StatusAbandoned
		if _, err := b.repo.Update(ctx, t); err != nil {
			return err
		}
	}
	return nil
}

func toBrief(tasks []domain.Task) []memport.BeastTask {
	out := make([]memport.BeastTask, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, memport.BeastTask{
			ID:         t.ID,
			Number:     t.Seq,
			Text:       t.Text,
			Minutes:    t.Minutes,
			DeadlineAt: t.DeadlineAt,
			Status:     string(t.Status),
			Expiries:   t.Expiries,
		})
	}
	return out
}
