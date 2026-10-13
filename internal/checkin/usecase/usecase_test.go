package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	camdomain "github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/checkin/domain"
	"github.com/an4eetos/decision-room/internal/checkin/port"
	"github.com/an4eetos/decision-room/internal/checkin/service"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

// ---- fakes ------------------------------------------------------------------

type memRepo struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.CheckIn
}

func newMemRepo() *memRepo { return &memRepo{rows: map[uuid.UUID]domain.CheckIn{}} }

func (m *memRepo) Create(_ context.Context, c domain.CheckIn) (domain.CheckIn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Slot == c.Slot && r.LocalDate.Equal(c.LocalDate) {
			return domain.CheckIn{}, port.ErrAlreadyFired
		}
	}
	c.ID, c.CreatedAt = uuid.New(), time.Now()
	m.rows[c.ID] = c
	return c, nil
}

func (m *memRepo) Exists(_ context.Context, slot string, date time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Slot == slot && r.LocalDate.Equal(date) {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (domain.CheckIn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return domain.CheckIn{}, port.ErrNotFound
	}
	return c, nil
}

func (m *memRepo) Unseen(context.Context, int) ([]domain.CheckIn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.CheckIn
	for _, r := range m.rows {
		if r.SeenAt == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memRepo) MarkSeen(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return port.ErrNotFound
	}
	now := time.Now()
	c.SeenAt = &now
	m.rows[id] = c
	return nil
}

func (m *memRepo) AttachSession(_ context.Context, id, session uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.rows[id]
	c.SessionID = &session
	now := time.Now()
	c.SeenAt = &now
	m.rows[id] = c
	return nil
}

func (m *memRepo) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

type fakeAnswerer struct {
	mu        sync.Mutex
	calls     int
	lastInput memusecase.ConsultInput
	err       error
}

func (f *fakeAnswerer) Execute(_ context.Context, in memusecase.ConsultInput) (memusecase.ConsultResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastInput = in
	if f.err != nil {
		return memusecase.ConsultResult{}, f.err
	}
	return memusecase.ConsultResult{Answer: "You said you'd call the landlord. Did you?"}, nil
}

type fakeSessions struct{ started int }

func (f *fakeSessions) StartSession(context.Context, string, string, string) (memusecase.ChatSessionDTO, error) {
	f.started++
	return memusecase.ChatSessionDTO{ID: uuid.New().String()}, nil
}

type fakeCommitments struct{ items []comdomain.Commitment }

func (f fakeCommitments) List(context.Context, []comdomain.Status) ([]comdomain.Commitment, error) {
	return f.items, nil
}

type fakeChats struct {
	memport.ChatRepository
	last *time.Time
}

func (f fakeChats) LastUserMessageAt(context.Context) (*time.Time, error) { return f.last, nil }

// ---- helpers ----------------------------------------------------------------

var almaty, _ = time.LoadLocation("Asia/Almaty")

func setup(now time.Time, items ...comdomain.Commitment) (*Generate, *memRepo, *fakeAnswerer, *fakeSessions) {
	repo, ans, sess := newMemRepo(), &fakeAnswerer{}, &fakeSessions{}
	g := NewGenerate(ans, sess, fakeCommitments{items: items}, repo, NewNotifier(""), almaty)
	g.now = func() time.Time { return now }
	return g, repo, ans, sess
}

func scheduler(g *Generate, now time.Time, last *time.Time) *Scheduler {
	slots, _ := service.ParseSlots("morning@08:00,evening@21:00")
	s := NewScheduler(g, fakeChats{last: last}, SchedulerConfig{
		Enabled: true, Slots: slots, Location: almaty,
		Grace: 90 * time.Minute, IdleAfter: 6 * time.Hour,
	})
	s.now = func() time.Time { return now }
	return s
}

func at(hour, minute int) time.Time { return time.Date(2026, 9, 26, hour, minute, 0, 0, almaty) }

// ---- tests ------------------------------------------------------------------

func TestTickFiresADueSlotOnce(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, ans, _ := setup(now)
	s := scheduler(g, now, nil)

	s.Tick(context.Background())
	s.Tick(context.Background())
	s.Tick(context.Background())

	if repo.count() != 1 {
		t.Fatalf("got %d check-ins from three ticks, want 1", repo.count())
	}
	// The exists check runs before the model call, so repeat ticks are free.
	if ans.calls != 1 {
		t.Fatalf("model called %d times, want 1", ans.calls)
	}
}

// A restart is a fresh scheduler with no memory. The database is what stops a
// second morning check-in.
func TestRestartDoesNotRefire(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, ans, _ := setup(now)
	scheduler(g, now, nil).Tick(context.Background())

	restarted := NewGenerate(ans, &fakeSessions{}, fakeCommitments{}, repo, NewNotifier(""), almaty)
	restarted.now = func() time.Time { return now }
	scheduler(restarted, now, nil).Tick(context.Background())

	if repo.count() != 1 {
		t.Fatalf("restart fired again: %d check-ins", repo.count())
	}
}

func TestDisabledSchedulerDoesNothing(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, _, _ := setup(now)
	s := scheduler(g, now, nil)
	s.enabled = false

	s.Tick(context.Background())
	if repo.count() != 0 {
		t.Fatal("fired while disabled")
	}
}

// The check-in is a short message ending in a question, so the mode's output
// template and the lenses must stay out — but its retrieval bias should not.
func TestScheduledCheckInIsPlainAndCheap(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	due := now.Add(-2 * time.Hour)
	g, _, ans, _ := setup(now, comdomain.Commitment{Text: "call the landlord", DueAt: &due, Status: comdomain.StatusOpen})
	scheduler(g, now, nil).Tick(context.Background())

	in := ans.lastInput
	if !in.Plain {
		t.Fatal("check-ins must drop the output template and lenses")
	}
	if in.Tier != "quick" {
		t.Fatalf("tier = %q; a check-in is one cheap call", in.Tier)
	}
	if in.ModeID != "day_plan" {
		t.Fatalf("morning should use day_plan for retrieval bias, got %q", in.ModeID)
	}
	if !strings.Contains(in.Question, "call the landlord") || !strings.Contains(in.Question, "overdue") {
		t.Fatalf("the question should name the overdue commitment:\n%s", in.Question)
	}
}

func TestIdleNudgeNamesTheMostUrgentCommitment(t *testing.T) {
	t.Parallel()

	now := at(15, 0)
	last := now.Add(-7 * time.Hour)
	dueToday := time.Date(2026, 9, 26, 23, 59, 0, 0, almaty)
	later := now.Add(10 * 24 * time.Hour)

	g, repo, ans, _ := setup(now,
		comdomain.Commitment{Text: "someday thing", DueAt: &later},
		comdomain.Commitment{Text: "finish the pricing", DueAt: &dueToday},
	)
	scheduler(g, now, &last).Tick(context.Background())

	items, _ := repo.Unseen(context.Background(), 5)
	if len(items) != 1 || items[0].Slot != "idle" {
		t.Fatalf("expected one idle nudge, got %+v", items)
	}
	if !strings.Contains(items[0].Body, "finish the pricing") {
		t.Fatalf("nudge should name the thing due today:\n%s", items[0].Body)
	}
	// Nudges are templated from what the database already knows: no model call.
	if ans.calls != 0 {
		t.Fatalf("idle nudge spent %d model calls", ans.calls)
	}
}

func TestIdleNudgeOnlyInsideActiveHours(t *testing.T) {
	t.Parallel()

	now := at(3, 0) // well outside 08:00–21:00
	last := now.Add(-12 * time.Hour)
	g, repo, _, _ := setup(now)
	scheduler(g, now, &last).Tick(context.Background())

	if repo.count() != 0 {
		t.Fatal("nudged at 3am")
	}
}

// Someone who has never written anything is new, not idle.
func TestNoIdleNudgeForNewUsers(t *testing.T) {
	t.Parallel()

	now := at(15, 0)
	g, repo, _, _ := setup(now)
	scheduler(g, now, nil).Tick(context.Background())

	if repo.count() != 0 {
		t.Fatal("nudged someone who has never used it")
	}
}

func TestIdleNudgeAtMostOncePerDay(t *testing.T) {
	t.Parallel()

	now := at(15, 0)
	last := now.Add(-7 * time.Hour)
	g, repo, _, _ := setup(now)
	s := scheduler(g, now, &last)

	s.Tick(context.Background())
	s.now = func() time.Time { return at(19, 0) }
	g.now = s.now
	s.Tick(context.Background())

	if repo.count() != 1 {
		t.Fatalf("got %d idle nudges in one day", repo.count())
	}
}

func TestStaleNudgeNamesTheItems(t *testing.T) {
	t.Parallel()

	now := at(12, 0)
	g, repo, _, _ := setup(now)
	s := scheduler(g, now, nil)

	s.OnStale(context.Background(), []comdomain.Commitment{{ID: uuid.New(), Text: "write the README"}})

	items, _ := repo.Unseen(context.Background(), 5)
	if len(items) != 1 || !strings.Contains(items[0].Body, "write the README") {
		t.Fatalf("got %+v", items)
	}
}

// Replying to a check-in creates its conversation once. Unanswered check-ins
// never create one, which keeps the chat list from filling with them.
func TestOpenCreatesTheSessionLazilyAndOnce(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, _, sessions := setup(now)
	scheduler(g, now, nil).Tick(context.Background())

	if sessions.started != 0 {
		t.Fatal("a conversation was created before anyone replied")
	}

	items, _ := repo.Unseen(context.Background(), 5)
	first, err := g.Open(context.Background(), items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := g.Open(context.Background(), items[0].ID)

	if first != second || sessions.started != 1 {
		t.Fatalf("opened %d sessions; opening twice should return the same one", sessions.started)
	}
	if left, _ := repo.Unseen(context.Background(), 5); len(left) != 0 {
		t.Fatal("an opened check-in should count as seen")
	}
}

func TestFailedGenerationIsRetriedOnTheNextTick(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, ans, _ := setup(now)
	ans.err = errors.New("429 quota")
	s := scheduler(g, now, nil)

	s.Tick(context.Background())
	if repo.count() != 0 {
		t.Fatal("a failed generation should not store anything")
	}

	ans.err = nil
	s.Tick(context.Background())
	if repo.count() != 1 {
		t.Fatal("the next tick, still inside grace, should succeed")
	}
}

// The notify command gets model-written text. It must be an argument, never
// something a shell could interpret.
func TestNotifierNeverUsesAShell(t *testing.T) {
	t.Parallel()

	n := NewNotifier("echo {title} {body}")
	if len(n.argv) != 3 || n.argv[0] != "echo" {
		t.Fatalf("argv = %v", n.argv)
	}
	// Runs without error; the payload is a literal argument to echo.
	n.Notify("hi", "; rm -rf / && echo pwned")
}

// ---- sitreps ----------------------------------------------------------------

type fakeSitreps struct {
	sitrep camdomain.Sitrep
	err    error
}

func (f fakeSitreps) Sitrep(context.Context) (camdomain.Sitrep, error) { return f.sitrep, f.err }

func mapWithWork() camdomain.Sitrep {
	front := uuid.New()
	return camdomain.Sitrep{
		Day: "2026-09-26",
		Fronts: []camdomain.SitrepFront{{
			ID: front, Name: "Work", Line: 0.25, OrdersOpen: 1, OrdersDue: 1,
			Objectives: []camdomain.SitrepObjective{{
				TheatrePost: camdomain.TheatrePost{ID: uuid.New(), Text: "Ship v2", Status: camdomain.StatusActive},
				Progress:    0.25,
				Orders:      []camdomain.SitrepOrder{{ID: uuid.New(), Text: "Write the tests", Due: "2026-09-26", DueToday: true}},
			}},
		}},
		Readings: []camdomain.Reading{{Kind: camdomain.ReadingAdvanced, Text: "◆ Ship v2 on Work is the closest to taken, 25% held."}},
	}
}

// With a map, a check-in is a sitrep: named for what it reports, with the
// map in the question and the intent drawn from map state. It stays plain
// and cheap.
func TestCheckInsBecomeSitrepsWhenThereIsAMap(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, ans, _ := setup(now)
	g.WithSitreps(fakeSitreps{sitrep: mapWithWork()})
	scheduler(g, now, nil).Tick(context.Background())

	items, _ := repo.Unseen(context.Background(), 5)
	if len(items) != 1 || items[0].Title != "Morning orders" {
		t.Fatalf("the morning check-in is the morning orders: %+v", items)
	}
	in := ans.lastInput
	if !in.Plain || in.Tier != "quick" || in.ModeID != "day_plan" {
		t.Fatalf("a sitrep is still one plain, quick call: %+v", in)
	}
	for _, want := range []string{"Write me a short sitrep", "point of main effort", "Front: Work — line 25%", "◆ Ship v2", "Write the tests (due today)", "closest to taken"} {
		if !strings.Contains(in.Question, want) {
			t.Fatalf("the sitrep question lacks %q:\n%s", want, in.Question)
		}
	}

	// The evening one reports gains and losses; "check in now" is a sitrep.
	evening := at(21, 5)
	g.now = func() time.Time { return evening }
	scheduler(g, evening, nil).Tick(context.Background())
	items, _ = repo.Unseen(context.Background(), 5)
	var titles []string
	for _, c := range items {
		titles = append(titles, c.Title)
	}
	if len(items) != 2 || !strings.Contains(strings.Join(titles, ","), "Evening gains and losses") {
		t.Fatalf("evening sitrep: %v", titles)
	}
	if !strings.Contains(ans.lastInput.Question, "Gains and losses") {
		t.Fatalf("evening intent:\n%s", ans.lastInput.Question)
	}
	c, err := g.Now(context.Background())
	if err != nil || c.Title != "Sitrep" {
		t.Fatalf("check in now with a map is a sitrep: %+v %v", c, err)
	}
}

// An empty map, or one that cannot be read, leaves the check-in as it was.
func TestCheckInsStayPlainWithoutAMap(t *testing.T) {
	t.Parallel()

	now := at(8, 5)
	g, repo, ans, _ := setup(now)
	g.WithSitreps(fakeSitreps{sitrep: camdomain.Sitrep{Empty: true}})
	scheduler(g, now, nil).Tick(context.Background())

	items, _ := repo.Unseen(context.Background(), 5)
	if len(items) != 1 || items[0].Title != "Morning check-in" {
		t.Fatalf("no map, no sitrep: %+v", items)
	}
	if strings.Contains(ans.lastInput.Question, "sitrep") || strings.Contains(ans.lastInput.Question, "Front:") {
		t.Fatalf("an empty map adds nothing:\n%s", ans.lastInput.Question)
	}

	broken, repo2, ans2, _ := setup(now)
	broken.WithSitreps(fakeSitreps{err: errors.New("db down")})
	scheduler(broken, now, nil).Tick(context.Background())
	if items, _ := repo2.Unseen(context.Background(), 5); len(items) != 1 || items[0].Title != "Morning check-in" {
		t.Fatalf("a map that cannot be read degrades to the plain check-in: %+v", items)
	}
	if ans2.calls != 1 {
		t.Fatal("the check-in itself still happens")
	}
}
