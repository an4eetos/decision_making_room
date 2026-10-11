package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/beast/domain"
	"github.com/an4eetos/decision-room/internal/beast/port"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

type memRepo struct {
	tasks []domain.Task
}

func (r *memRepo) Create(_ context.Context, t domain.Task) (domain.Task, error) {
	t.ID = uuid.New()
	for _, existing := range r.tasks {
		if existing.SessionID == t.SessionID && existing.Seq >= t.Seq {
			t.Seq = existing.Seq
		}
	}
	t.Seq++
	r.tasks = append(r.tasks, t)
	return t, nil
}

func (r *memRepo) Get(_ context.Context, id uuid.UUID) (domain.Task, error) {
	for _, t := range r.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return domain.Task{}, port.ErrNotFound
}

func (r *memRepo) ListBySession(_ context.Context, sessionID uuid.UUID) ([]domain.Task, error) {
	var out []domain.Task
	for _, t := range r.tasks {
		if t.SessionID == sessionID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *memRepo) Update(_ context.Context, t domain.Task) (domain.Task, error) {
	for i := range r.tasks {
		if r.tasks[i].ID == t.ID {
			r.tasks[i] = t
			return t, nil
		}
	}
	return domain.Task{}, port.ErrNotFound
}

func (r *memRepo) Rearm(_ context.Context, id uuid.UUID, now, deadline time.Time) (domain.Task, bool, error) {
	for i := range r.tasks {
		t := &r.tasks[i]
		if t.ID == id && t.Status == domain.StatusOpen && !t.DeadlineAt.After(now) {
			t.Expiries++
			t.DeadlineAt = deadline
			return *t, true, nil
		}
	}
	return domain.Task{}, false, nil
}

type memLoops struct {
	loops []port.Loop
	done  []uuid.UUID
}

func (l *memLoops) DueBy(_ context.Context, by time.Time) ([]port.Loop, error) { return l.loops, nil }
func (l *memLoops) Done(_ context.Context, id uuid.UUID) error {
	l.done = append(l.done, id)
	return nil
}

var t0 = time.Date(2026, 10, 12, 18, 0, 0, 0, time.UTC)

func newBoard(loops port.Loops) (*Board, *memRepo, *time.Time) {
	repo := &memRepo{}
	clock := t0
	b := NewBoard(repo, loops)
	b.now = func() time.Time { return clock }
	return b, repo, &clock
}

func TestApplyPutsOrdersOnTheClockOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	loopID := uuid.New()
	b, repo, _ := newBoard(&memLoops{})
	session, message := uuid.New(), uuid.New()
	loops := []memport.BeastLoop{{ID: loopID, Number: 1, Text: "Visa form"}}

	err := b.Apply(ctx, session, message, []memport.BeastOrder{
		{Minutes: 20, Text: "Fill the visa form", Loop: 1},
		{Minutes: 5, Text: "Open the laptop"},
	}, nil, loops)
	if err != nil {
		t.Fatal(err)
	}
	// The next turn repeats an order that is already on the board.
	if err := b.Apply(ctx, session, message, []memport.BeastOrder{{Minutes: 10, Text: "open the LAPTOP"}}, nil, nil); err != nil {
		t.Fatal(err)
	}

	if len(repo.tasks) != 2 {
		t.Fatalf("got %d tasks, want 2: a repeated order is not a new clock", len(repo.tasks))
	}
	first := repo.tasks[0]
	if first.Seq != 1 || repo.tasks[1].Seq != 2 {
		t.Errorf("numbers %d, %d; want 1, 2", first.Seq, repo.tasks[1].Seq)
	}
	if !first.DeadlineAt.Equal(t0.Add(20 * time.Minute)) {
		t.Errorf("deadline %v, want twenty minutes from now", first.DeadlineAt)
	}
	if first.CommitmentID == nil || *first.CommitmentID != loopID {
		t.Error("an order naming loop 1 should be linked to that loop")
	}
	if repo.tasks[1].CommitmentID != nil {
		t.Error("an order naming no loop is linked to none")
	}
}

func TestReportingDoneClosesTheLoopToo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	loopID := uuid.New()
	loops := &memLoops{}
	b, repo, _ := newBoard(loops)
	session := uuid.New()

	_ = b.Apply(ctx, session, uuid.New(), []memport.BeastOrder{{Minutes: 20, Text: "Fill the form", Loop: 1}},
		nil, []memport.BeastLoop{{ID: loopID, Number: 1}})
	task := repo.tasks[0]

	reported, err := b.Report(ctx, session, []uuid.UUID{task.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 || reported[0].Status != "done" {
		t.Fatalf("reported %+v", reported)
	}
	if len(loops.done) != 1 || loops.done[0] != loopID {
		t.Fatalf("closed loops %v, want the linked one", loops.done)
	}

	// A second report of the same task, as when the click lands before the
	// report turn, is still about that task but closes nothing twice.
	again, err := b.Report(ctx, session, []uuid.UUID{task.ID})
	if err != nil || len(again) != 1 {
		t.Fatalf("second report: %+v, %v", again, err)
	}
	if len(loops.done) != 1 {
		t.Fatal("the loop was closed twice")
	}

	// Another session's task cannot be reported from here.
	if other, _ := b.Report(ctx, uuid.New(), []uuid.UUID{task.ID}); len(other) != 0 {
		t.Fatal("a task was reported from the wrong conversation")
	}
}

func TestDoneByNumberFromTheModel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, repo, _ := newBoard(nil)
	session := uuid.New()
	_ = b.Apply(ctx, session, uuid.New(), []memport.BeastOrder{{Minutes: 5, Text: "a"}, {Minutes: 5, Text: "b"}}, nil, nil)

	if err := b.Apply(ctx, session, uuid.New(), nil, []int{2, 7}, nil); err != nil {
		t.Fatal(err)
	}
	if repo.tasks[0].Status != domain.StatusOpen || repo.tasks[1].Status != domain.StatusDone {
		t.Fatalf("statuses %s, %s; want open, done", repo.tasks[0].Status, repo.tasks[1].Status)
	}
}

func TestExpireRearmsShorterAndOnlyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, repo, clock := newBoard(nil)
	session := uuid.New()
	_ = b.Apply(ctx, session, uuid.New(), []memport.BeastOrder{{Minutes: 20, Text: "late"}, {Minutes: 60, Text: "fine"}}, nil, nil)

	// Nothing is late yet.
	if got, _ := b.Expire(ctx, session, *clock); len(got) != 0 {
		t.Fatalf("expired %+v before any deadline", got)
	}

	*clock = t0.Add(21 * time.Minute)
	got, err := b.Expire(ctx, session, *clock)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "late" || got[0].Expiries != 1 {
		t.Fatalf("expired %+v; want only the late one, counted once", got)
	}
	if want := clock.Add(10 * time.Minute); !repo.tasks[0].DeadlineAt.Equal(want) {
		t.Errorf("re-armed to %v, want %v (half of twenty)", repo.tasks[0].DeadlineAt, want)
	}

	// A second tab asking at the same moment finds nothing left to expire.
	if again, _ := b.Expire(ctx, session, *clock); len(again) != 0 {
		t.Fatalf("expired twice: %+v", again)
	}
}

func TestResolvedTasksStayResolved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, repo, _ := newBoard(nil)
	session := uuid.New()
	_ = b.Apply(ctx, session, uuid.New(), []memport.BeastOrder{{Minutes: 5, Text: "a"}, {Minutes: 5, Text: "b"}}, nil, nil)
	done, abandoned := repo.tasks[0], repo.tasks[1]

	if _, err := b.Complete(ctx, done.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Abandon(ctx, abandoned.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := b.Abandon(ctx, done.ID); !errors.Is(err, ErrBadTransition) {
		t.Errorf("abandoning a done task: %v", err)
	}
	if _, err := b.Complete(ctx, abandoned.ID); !errors.Is(err, ErrBadTransition) {
		t.Errorf("completing an abandoned task: %v", err)
	}
	minutes := 10
	if _, err := b.Edit(ctx, done.ID, EditInput{Minutes: &minutes}); !errors.Is(err, ErrBadTransition) {
		t.Errorf("editing a done task: %v", err)
	}
}

func TestEditNeverPullsAClockIntoThePast(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, repo, _ := newBoard(nil)
	session := uuid.New()
	_ = b.Apply(ctx, session, uuid.New(), []memport.BeastOrder{{Minutes: 5, Text: "a"}}, nil, nil)

	got, err := b.Edit(ctx, repo.tasks[0].ID, EditInput{Extend: -30})
	if err != nil {
		t.Fatal(err)
	}
	if want := t0.Add(time.Minute); !got.DeadlineAt.Equal(want) {
		t.Fatalf("deadline %v, want a minute from now", got.DeadlineAt)
	}

	minutes := 45
	got, _ = b.Edit(ctx, got.ID, EditInput{Minutes: &minutes})
	if got.Minutes != 45 || !got.DeadlineAt.Equal(t0.Add(45*time.Minute)) {
		t.Fatalf("a new length should start a fresh clock, got %d min until %v", got.Minutes, got.DeadlineAt)
	}
}

func TestStandDownAbandonsWhatIsLeft(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b, repo, _ := newBoard(nil)
	session := uuid.New()
	_ = b.Apply(ctx, session, uuid.New(), []memport.BeastOrder{{Minutes: 5, Text: "a"}, {Minutes: 5, Text: "b"}}, nil, nil)
	_, _ = b.Complete(ctx, repo.tasks[0].ID)

	if err := b.StandDown(ctx, session); err != nil {
		t.Fatal(err)
	}
	if repo.tasks[0].Status != domain.StatusDone || repo.tasks[1].Status != domain.StatusAbandoned {
		t.Fatalf("statuses %s, %s; want done, abandoned", repo.tasks[0].Status, repo.tasks[1].Status)
	}
}

func TestDueLoopsAreNumberedAndCapped(t *testing.T) {
	t.Parallel()
	var loops memLoops
	for range 7 {
		loops.loops = append(loops.loops, port.Loop{ID: uuid.New(), Text: "x"})
	}
	b, _, _ := newBoard(&loops)

	got, err := b.DueLoops(context.Background(), t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != maxLoops || got[0].Number != 1 || got[maxLoops-1].Number != maxLoops {
		t.Fatalf("got %d loops numbered %d..%d", len(got), got[0].Number, got[len(got)-1].Number)
	}

	none, _ := NewBoard(&memRepo{}, nil).DueLoops(context.Background(), t0)
	if none != nil {
		t.Fatal("without loops wired the brief has none")
	}
}
