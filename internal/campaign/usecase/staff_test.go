package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

type memAppraisals struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.Appraisal
}

func newMemAppraisals() *memAppraisals { return &memAppraisals{rows: map[uuid.UUID]domain.Appraisal{}} }

func (m *memAppraisals) CreateAppraisal(_ context.Context, a domain.Appraisal) (domain.Appraisal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a.ID, a.CreatedAt = uuid.New(), time.Now()
	m.rows[a.ID] = a
	return a, nil
}

func (m *memAppraisals) GetAppraisal(_ context.Context, id uuid.UUID) (domain.Appraisal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.rows[id]
	if !ok {
		return domain.Appraisal{}, port.ErrNotFound
	}
	return a, nil
}

func (m *memAppraisals) ListAppraisals(context.Context, int) ([]domain.Appraisal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Appraisal
	for _, a := range m.rows {
		out = append(out, a)
	}
	return out, nil
}

func (m *memAppraisals) AttachAppraisalSession(_ context.Context, id, session uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.rows[id]
	if !ok {
		return port.ErrNotFound
	}
	a.SessionID = &session
	m.rows[id] = a
	return nil
}

type fakeAnswerer struct {
	mu    sync.Mutex
	calls int
	last  memusecase.ConsultInput
	err   error
	block chan struct{}
}

func (f *fakeAnswerer) Execute(_ context.Context, in memusecase.ConsultInput) (memusecase.ConsultResult, error) {
	f.mu.Lock()
	f.calls++
	f.last = in
	block := f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	if f.err != nil {
		return memusecase.ConsultResult{}, f.err
	}
	return memusecase.ConsultResult{
		Answer:   "**Point of main effort**\n◆ Ship v2.",
		Tier:     "standard",
		Generals: []string{"kutuzov", "manstein"},
	}, nil
}

type fakeSessions struct {
	started  int
	generals []string
	title    string
}

func (f *fakeSessions) StartSessionWith(_ context.Context, title, _, _ string, generals []string) (memusecase.ChatSessionDTO, error) {
	f.started++
	f.generals = generals
	f.title = title
	return memusecase.ChatSessionDTO{ID: uuid.New().String()}, nil
}

func staffSetup(t *testing.T) (*Staff, *Manage, *fakeAnswerer, *fakeSessions) {
	t.Helper()
	repo, orders := newMemRepo(), newMemOrders()
	manage := NewManage(repo, comusecase.NewManage(orders))
	snaps := NewSnapshots(manage, newMemSnapshots(), time.UTC, time.Minute)
	sitreps := NewSitreps(manage, snaps, time.UTC, true)
	ans, sess := &fakeAnswerer{}, &fakeSessions{}
	return NewStaff(sitreps, manage, newMemAppraisals(), ans, sess), manage, ans, sess
}

func TestAppraiseNeedsAMapAndStoresWhatTheStaffSaid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	staff, manage, ans, _ := staffSetup(t)

	if _, err := staff.Appraise(ctx, AppraiseInput{}); !errors.Is(err, ErrNothingToAppraise) {
		t.Fatalf("an empty map has nothing to appraise, got %v", err)
	}
	if ans.calls != 0 {
		t.Fatal("no model call on an empty map")
	}

	work, _ := manage.CreateFront(ctx, "Work")
	health, _ := manage.CreateFront(ctx, "Health")
	if _, err := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship v2", FrontID: &work.ID}); err != nil {
		t.Fatal(err)
	}

	a, err := staff.Appraise(ctx, AppraiseInput{})
	if err != nil {
		t.Fatal(err)
	}
	if a.Tier != "standard" || len(a.Generals) != 2 || !strings.Contains(a.Body, "Point of main effort") || a.FrontID != nil {
		t.Fatalf("stored appraisal: %+v", a)
	}
	in := ans.last
	if in.ModeID != "staff" || in.Tier != "standard" || in.Plain {
		t.Fatalf("the staff mode, at the asked depth, with its lenses: %+v", in)
	}
	if !strings.Contains(in.Question, "Appraise my campaign map") || !strings.Contains(in.Question, "Ship v2") {
		t.Fatalf("the question names the objectives in play:\n%s", in.Question)
	}

	// One front: the question narrows, and an empty front has nothing to say.
	if _, err := staff.Appraise(ctx, AppraiseInput{FrontID: &health.ID, Tier: "deep"}); !errors.Is(err, ErrNothingToAppraise) {
		t.Fatalf("an empty front: %v", err)
	}
	a, err = staff.Appraise(ctx, AppraiseInput{FrontID: &work.ID, Tier: "deep"})
	if err != nil {
		t.Fatal(err)
	}
	if a.FrontID == nil || *a.FrontID != work.ID || ans.last.Tier != "deep" || !strings.Contains(ans.last.Question, "Appraise the Work front") {
		t.Fatalf("a front appraisal: %+v\n%s", a, ans.last.Question)
	}
	unknown := uuid.New()
	if _, err := staff.Appraise(ctx, AppraiseInput{FrontID: &unknown}); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("an unknown front is not found: %v", err)
	}

	list, _ := staff.Appraisals(ctx, 10)
	if len(list) != 2 {
		t.Fatalf("two appraisals stored, got %d", len(list))
	}
}

func TestAppraiseRefusesASecondWhileOneIsWritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	staff, manage, ans, _ := staffSetup(t)
	work, _ := manage.CreateFront(ctx, "Work")
	_, _ = manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship v2", FrontID: &work.ID})

	ans.block = make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := staff.Appraise(ctx, AppraiseInput{})
		done <- err
	}()
	// Wait for the first to reach the model.
	for {
		ans.mu.Lock()
		started := ans.calls == 1
		ans.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := staff.Appraise(ctx, AppraiseInput{}); !errors.Is(err, ErrStaffBusy) {
		t.Fatalf("a second appraisal while one runs is refused, got %v", err)
	}
	close(ans.block)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := staff.Appraise(ctx, AppraiseInput{}); err != nil {
		t.Fatalf("once the first is done the staff is free: %v", err)
	}
}

func TestOpenSeatsTheGeneralsOnceAndOnlyWhenAsked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	staff, manage, _, sessions := staffSetup(t)
	work, _ := manage.CreateFront(ctx, "Work")
	_, _ = manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship v2", FrontID: &work.ID})

	a, err := staff.Appraise(ctx, AppraiseInput{FrontID: &work.ID})
	if err != nil {
		t.Fatal(err)
	}
	if sessions.started != 0 {
		t.Fatal("an appraisal you only read opens no conversation")
	}

	first, err := staff.Open(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := staff.Open(ctx, a.ID)
	if first != second || sessions.started != 1 {
		t.Fatalf("opening twice is one conversation; started %d", sessions.started)
	}
	if len(sessions.generals) != 2 || sessions.generals[0] != "kutuzov" {
		t.Fatalf("the generals who wrote it are seated: %v", sessions.generals)
	}
	if !strings.Contains(sessions.title, "Work") {
		t.Fatalf("a front appraisal's conversation names the front: %q", sessions.title)
	}
	if _, err := staff.Open(ctx, uuid.New()); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("unknown appraisal: %v", err)
	}
}

// The page asks and comes straight back; the appraisal lands in the store
// on its own time and Status says how it went.
func TestStartAppraisesInTheBackground(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	staff, manage, ans, _ := staffSetup(t)

	if err := staff.Start(ctx, AppraiseInput{}); !errors.Is(err, ErrNothingToAppraise) {
		t.Fatalf("an empty map is refused before anything starts: %v", err)
	}
	if staff.Status().Busy {
		t.Fatal("a refused start leaves the staff free")
	}

	work, _ := manage.CreateFront(ctx, "Work")
	_, _ = manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship v2", FrontID: &work.ID})

	ans.block = make(chan struct{})
	if err := staff.Start(ctx, AppraiseInput{Tier: "deep"}); err != nil {
		t.Fatal(err)
	}
	status := staff.Status()
	if !status.Busy || status.Since.IsZero() {
		t.Fatalf("started: %+v", status)
	}
	if err := staff.Start(ctx, AppraiseInput{}); !errors.Is(err, ErrStaffBusy) {
		t.Fatalf("a second start while one runs is refused: %v", err)
	}
	if _, err := staff.Appraise(ctx, AppraiseInput{}); !errors.Is(err, ErrStaffBusy) {
		t.Fatalf("so is a synchronous one: %v", err)
	}

	close(ans.block)
	waitFree := func() StaffStatus {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if s := staff.Status(); !s.Busy {
				return s
			}
			if time.Now().After(deadline) {
				t.Fatal("the appraisal never finished")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if s := waitFree(); s.Error != "" {
		t.Fatalf("a finished appraisal reports no error: %+v", s)
	}
	list, _ := staff.Appraisals(ctx, 10)
	if len(list) != 1 || list[0].Tier != "standard" || ans.last.Tier != "deep" {
		t.Fatalf("stored at the depth asked: %+v / %+v", list, ans.last)
	}

	// A failure is kept on the status until the next start, so a page that
	// was not watching still learns why nothing arrived.
	ans.err = errors.New("model overloaded")
	if err := staff.Start(ctx, AppraiseInput{}); err != nil {
		t.Fatal(err)
	}
	if s := waitFree(); !strings.Contains(s.Error, "model overloaded") {
		t.Fatalf("the failure should be reported: %+v", s)
	}
	ans.err = nil
	if err := staff.Start(ctx, AppraiseInput{}); err != nil {
		t.Fatal(err)
	}
	if s := staff.Status(); s.Error != "" {
		t.Fatalf("a new start clears the old failure: %+v", s)
	}
	waitFree()
}
