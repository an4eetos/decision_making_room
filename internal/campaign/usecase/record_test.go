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
	memport "github.com/an4eetos/decision-room/internal/memory/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

func TestRecordQuarterLifeAndSeal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	items := newMemRepo()
	manage := NewManage(items, comusecase.NewManage(newMemOrders()))
	manage.now = func() time.Time { return clock }
	rec := NewRecord(newMemRecord(), items, time.UTC)
	rec.now = manage.now

	opened, err := rec.Open(ctx, OpenInput{})
	if err != nil || opened.Quarter != "2026-Q4" || opened.Phase != domain.PhasePlanning || opened.Name != "2026 Q4" {
		t.Fatalf("open this quarter: %+v %v", opened, err)
	}
	if _, err := rec.Open(ctx, OpenInput{}); !errors.Is(err, port.ErrDuplicate) {
		t.Fatalf("one campaign per quarter: %v", err)
	}
	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseReview); !errors.Is(err, ErrPhase) {
		t.Fatalf("planning does not skip to review: %v", err)
	}

	front, err := manage.CreateFront(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	ship, err := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship the beta", FrontID: &front.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Assign(ctx, opened.ID, ship.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manage.Update(ctx, ship.ID, Patch{Status: statusPtr(domain.StatusResolved)}); err != nil {
		t.Fatal(err)
	}

	page, err := rec.Page(ctx)
	if err != nil || len(page.Campaigns) != 1 || len(page.Campaigns[0].Commendations) != 1 {
		t.Fatalf("a taken objective is a commendation: %+v %v", page, err)
	}
	if page.Campaigns[0].Commendations[0].Kind != domain.CommendationTaken {
		t.Fatalf("kind: %+v", page.Campaigns[0].Commendations[0])
	}

	if _, err := manage.Update(ctx, ship.ID, Patch{Status: statusPtr(domain.StatusActive)}); err != nil {
		t.Fatal(err)
	}
	page, _ = rec.Page(ctx)
	if len(page.Campaigns[0].Commendations) != 0 {
		t.Fatalf("restoring an objective before close removes the line: %+v", page.Campaigns[0].Commendations)
	}

	if _, err := manage.Update(ctx, ship.ID, Patch{Status: statusPtr(domain.StatusResolved)}); err != nil {
		t.Fatal(err)
	}
	ops, err := rec.Advance(ctx, opened.ID, domain.PhaseOperations)
	if err != nil || ops.Phase != domain.PhaseOperations {
		t.Fatal(err)
	}
	reviewing, err := rec.Advance(ctx, opened.ID, domain.PhaseReview)
	if err != nil || reviewing.Phase != domain.PhaseReview {
		t.Fatal(err)
	}
	closed, err := rec.Advance(ctx, opened.ID, domain.PhaseClosed)
	if err != nil || closed.Phase != domain.PhaseClosed || closed.ClosedAt == nil {
		t.Fatalf("close seals: %+v %v", closed, err)
	}

	if _, err := manage.Update(ctx, ship.ID, Patch{Status: statusPtr(domain.StatusActive)}); err != nil {
		t.Fatal(err)
	}
	page, _ = rec.Page(ctx)
	if len(page.Campaigns[0].Commendations) != 1 || page.Campaigns[0].Commendations[0].Kind != domain.CommendationTaken {
		t.Fatalf("a closed quarter keeps the line after the objective is restored: %+v", page.Campaigns[0].Commendations)
	}
	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseOperations); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed is final: %v", err)
	}
	if err := rec.Delete(ctx, opened.ID); !errors.Is(err, ErrClosed) {
		t.Fatalf("a closed quarter is not deleted: %v", err)
	}
}

func TestRecordPredictionsAndClosedRescore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	items := newMemRepo()
	rec := NewRecord(newMemRecord(), items, time.UTC)
	rec.now = func() time.Time { return clock }

	opened, err := rec.Open(ctx, OpenInput{Name: "Ship"})
	if err != nil {
		t.Fatal(err)
	}
	front, _ := NewManage(items, comusecase.NewManage(newMemOrders())).CreateFront(ctx, "Work")
	ship, err := NewManage(items, comusecase.NewManage(newMemOrders())).CreateItem(ctx, CreateInput{
		Type: domain.TypeObjective, Text: "Ship the beta", FrontID: &front.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Assign(ctx, opened.ID, ship.ID); err != nil {
		t.Fatal(err)
	}

	turn := memport.Turn{
		SessionID: uuid.New(), MessageID: uuid.New(), ModeID: preMortemModeID,
		AssistantText: "**How it failed**\n- The beta shipped without tests and broke.\n- The lease ran out.\n\n**Early warnings**\n- A week with no test run.\n",
	}
	if err := rec.FromPreMortem(ctx, turn); err != nil {
		t.Fatal(err)
	}
	if err := rec.FromPreMortem(ctx, turn); err != nil {
		t.Fatal(err)
	}
	page, err := rec.Page(ctx)
	if err != nil || len(page.Campaigns[0].Predictions) != 2 {
		t.Fatalf("a repeated pre-mortem does not file the failure twice: %+v %v", page.Campaigns, err)
	}
	pred := page.Campaigns[0].Predictions
	var aimed domain.Prediction
	for _, p := range pred {
		if strings.Contains(p.Text, "beta") {
			aimed = p
		}
		if p.Warning != "" {
			t.Fatalf("two failures and one warning are not zipped: %+v", p)
		}
	}
	if aimed.ObjectiveID == nil || *aimed.ObjectiveID != ship.ID {
		t.Fatalf("the failure that shares a word is aimed at the objective: %+v", aimed)
	}

	kept, err := rec.PatchPrediction(ctx, aimed.ID, PredictionPatch{
		Status: statusPred(domain.PredictionKept), Chance: chancePtr(domain.ChanceLikely),
	})
	if err != nil || kept.Status != domain.PredictionKept || kept.Chance != domain.ChanceLikely {
		t.Fatalf("keep with a chance: %+v %v", kept, err)
	}
	scored, err := rec.PatchPrediction(ctx, aimed.ID, PredictionPatch{Outcome: outcomePtr(domain.OutcomeCalled)})
	if err != nil || scored.Outcome != domain.OutcomeCalled || scored.ScoredAt == nil {
		t.Fatalf("score: %+v %v", scored, err)
	}
	page, _ = rec.Page(ctx)
	if !hasKind(page.Campaigns[0].Commendations, domain.CommendationCalled) {
		t.Fatalf("a prediction that called it is a commendation: %+v", page.Campaigns[0].Commendations)
	}

	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseOperations); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseReview); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseClosed); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.PatchPrediction(ctx, aimed.ID, PredictionPatch{Outcome: outcomePtr(domain.OutcomeMissed)}); !errors.Is(err, ErrClosed) {
		t.Fatalf("a closed quarter's prediction is not rescored: %v", err)
	}
}

func TestRecordNoteAndAfterAction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	items := newMemRepo()
	rec := NewRecord(newMemRecord(), items, time.UTC)
	rec.now = func() time.Time { return clock }

	if note, err := rec.Note(ctx); err != nil || note != nil {
		t.Fatalf("no campaign is no reading: %+v %v", note, err)
	}

	opened, err := rec.Open(ctx, OpenInput{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		pred, err := rec.repo.CreatePrediction(ctx, domain.Prediction{
			CampaignID: &opened.ID, Text: "failure " + string(rune('a'+i)), Status: domain.PredictionProposed,
			Source: preMortemSource, Fingerprint: "fp-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rec.PatchPrediction(ctx, pred.ID, PredictionPatch{
			Status: statusPred(domain.PredictionKept), Chance: chancePtr(domain.ChanceEven),
			Outcome: outcomePtr(domain.OutcomeMissed),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseOperations); err != nil {
		t.Fatal(err)
	}
	note, err := rec.Note(ctx)
	if err != nil || note == nil || note.Calibration.Score == nil {
		t.Fatalf("five scored predictions are a number during operations: %+v %v", note, err)
	}

	if _, err := rec.Advance(ctx, opened.ID, domain.PhaseReview); err != nil {
		t.Fatal(err)
	}
	answer := &scriptedAnswer{answer: "**What the quarter took**\nNone.\n", started: make(chan struct{}), release: make(chan struct{})}
	rec.WithConsult(answer)
	if err := rec.StartReview(ctx, opened.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-answer.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the after-action did not start")
	}
	if err := rec.StartReview(ctx, opened.ID); !errors.Is(err, ErrReviewBusy) {
		t.Fatalf("a second write is refused: %v", err)
	}
	close(answer.release)

	deadline := time.Now().Add(2 * time.Second)
	var body string
	for time.Now().Before(deadline) {
		page, err := rec.Page(ctx)
		if err != nil {
			t.Fatal(err)
		}
		body = page.Campaigns[0].Campaign.Review
		if body != "" || page.Review.Error != "" {
			if page.Review.Error != "" {
				t.Fatal(page.Review.Error)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if body == "" || !strings.Contains(answer.question(), "Calibration:") || !strings.Contains(answer.question(), "Do not add an outcome") {
		t.Fatalf("after-action stored %q from question %q", body, answer.question())
	}
}

func TestSitrepMentionsTheRecordOnlyWhenItHasSomethingToSay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	items := newMemRepo()
	manage := NewManage(items, comusecase.NewManage(newMemOrders()))
	sitreps := NewSitreps(manage, nil, time.UTC, true)
	front, _ := manage.CreateFront(ctx, "Work")
	if _, err := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship the beta", FrontID: &front.ID}); err != nil {
		t.Fatal(err)
	}

	sitreps.WithRecord(noteSource{note: nil})
	brief, err := sitreps.Situation(ctx)
	if err != nil || strings.Contains(brief, "unscored") || strings.Contains(brief, "Calibration") {
		t.Fatalf("an empty record adds no line:\n%s %v", brief, err)
	}

	sitreps.WithRecord(noteSource{note: &domain.RecordNote{
		Quarter: "2026-Q4", Phase: domain.PhaseReview, Unscored: 2,
	}})
	brief, err = sitreps.Situation(ctx)
	if err != nil || !strings.Contains(brief, "2 pre-mortem predictions on 2026 Q4 are still unscored") {
		t.Fatalf("review with unscored predictions is a reading:\n%s %v", brief, err)
	}
}

type noteSource struct{ note *domain.RecordNote }

func (n noteSource) Note(context.Context) (*domain.RecordNote, error) { return n.note, nil }

type scriptedAnswer struct {
	mu      sync.Mutex
	once    sync.Once
	q       string
	answer  string
	started chan struct{}
	release chan struct{}
}

func (s *scriptedAnswer) Execute(_ context.Context, in memusecase.ConsultInput) (memusecase.ConsultResult, error) {
	s.mu.Lock()
	s.q = in.Question
	s.mu.Unlock()
	s.once.Do(func() { close(s.started) })
	<-s.release
	return memusecase.ConsultResult{Answer: s.answer, Tier: "standard", Generals: []string{"rokossovsky", "zhukov"}}, nil
}

func (s *scriptedAnswer) question() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.q
}

func statusPtr(s domain.Status) *domain.Status { return &s }

func statusPred(s domain.PredictionStatus) *domain.PredictionStatus { return &s }

func chancePtr(c domain.Chance) *domain.Chance { return &c }

func outcomePtr(o domain.Outcome) *domain.Outcome { return &o }

func hasKind(lines []domain.Commendation, kind domain.CommendationKind) bool {
	for _, line := range lines {
		if line.Kind == kind {
			return true
		}
	}
	return false
}

// memRecord is the record repository in memory, with the same duplicate and
// closed-quarter rules the SQL checks enforce.
type memRecord struct {
	mu        sync.Mutex
	campaigns map[uuid.UUID]domain.Campaign
	assigned  map[uuid.UUID][]uuid.UUID
	preds     map[uuid.UUID]domain.Prediction
	lines     map[uuid.UUID][]domain.Commendation
}

func newMemRecord() *memRecord {
	return &memRecord{
		campaigns: map[uuid.UUID]domain.Campaign{},
		assigned:  map[uuid.UUID][]uuid.UUID{},
		preds:     map[uuid.UUID]domain.Prediction{},
		lines:     map[uuid.UUID][]domain.Commendation{},
	}
}

func (m *memRecord) CreateCampaign(_ context.Context, c domain.Campaign) (domain.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, other := range m.campaigns {
		if other.Quarter == c.Quarter {
			return domain.Campaign{}, port.ErrDuplicate
		}
	}
	c.ID = uuid.New()
	now := time.Now()
	c.CreatedAt, c.UpdatedAt = now, now
	m.campaigns[c.ID] = c
	return c, nil
}

func (m *memRecord) GetCampaign(_ context.Context, id uuid.UUID) (domain.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.campaigns[id]
	if !ok {
		return domain.Campaign{}, port.ErrNotFound
	}
	return c, nil
}

func (m *memRecord) UpdateCampaign(_ context.Context, c domain.Campaign) (domain.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.campaigns[c.ID]
	if !ok || cur.Phase == domain.PhaseClosed {
		return domain.Campaign{}, port.ErrNotFound
	}
	c.CreatedAt, c.UpdatedAt = cur.CreatedAt, time.Now()
	c.OpenedAt, c.ClosedAt = cur.OpenedAt, cur.ClosedAt
	c.Calibration, c.CalibrationN = cur.Calibration, cur.CalibrationN
	m.campaigns[c.ID] = c
	return c, nil
}

func (m *memRecord) DeleteCampaign(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.campaigns[id]
	if !ok || c.Phase == domain.PhaseClosed {
		return port.ErrNotFound
	}
	delete(m.campaigns, id)
	delete(m.assigned, id)
	for predID, pred := range m.preds {
		if pred.CampaignID != nil && *pred.CampaignID == id {
			pred.CampaignID = nil
			m.preds[predID] = pred
		}
	}
	return nil
}

func (m *memRecord) ListCampaigns(context.Context) ([]domain.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Campaign
	for _, c := range m.campaigns {
		out = append(out, c)
	}
	return out, nil
}

func (m *memRecord) AssignObjective(_ context.Context, campaignID, itemID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.campaigns[campaignID]; !ok {
		return port.ErrNotFound
	}
	for _, id := range m.assigned[campaignID] {
		if id == itemID {
			return port.ErrDuplicate
		}
	}
	m.assigned[campaignID] = append(m.assigned[campaignID], itemID)
	return nil
}

func (m *memRecord) UnassignObjective(_ context.Context, campaignID, itemID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.assigned[campaignID]
	for i, id := range ids {
		if id == itemID {
			m.assigned[campaignID] = append(ids[:i], ids[i+1:]...)
			return nil
		}
	}
	return port.ErrNotFound
}

func (m *memRecord) ListAssignments(_ context.Context, campaignID uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]uuid.UUID(nil), m.assigned[campaignID]...), nil
}

func (m *memRecord) CreatePrediction(_ context.Context, p domain.Prediction) (domain.Prediction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.Status.Live() && p.Fingerprint != "" {
		for _, other := range m.preds {
			if other.Status.Live() && other.Fingerprint == p.Fingerprint {
				return domain.Prediction{}, port.ErrDuplicate
			}
		}
	}
	p.ID = uuid.New()
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	m.preds[p.ID] = p
	return p, nil
}

func (m *memRecord) GetPrediction(_ context.Context, id uuid.UUID) (domain.Prediction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.preds[id]
	if !ok {
		return domain.Prediction{}, port.ErrNotFound
	}
	return p, nil
}

func (m *memRecord) UpdatePrediction(_ context.Context, p domain.Prediction) (domain.Prediction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.preds[p.ID]; !ok {
		return domain.Prediction{}, port.ErrNotFound
	}
	p.UpdatedAt = time.Now()
	m.preds[p.ID] = p
	return p, nil
}

func (m *memRecord) ListPredictions(context.Context) ([]domain.Prediction, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Prediction
	for _, p := range m.preds {
		out = append(out, p)
	}
	return out, nil
}

func (m *memRecord) SealCampaign(_ context.Context, c domain.Campaign, lines []domain.Commendation) (domain.Campaign, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.campaigns[c.ID]
	if !ok || cur.Phase == domain.PhaseClosed {
		return domain.Campaign{}, port.ErrNotFound
	}
	c.Phase = domain.PhaseClosed
	c.CreatedAt, c.UpdatedAt = cur.CreatedAt, time.Now()
	c.OpenedAt = cur.OpenedAt
	c.Note, c.Review, c.ReviewAt = cur.Note, cur.Review, cur.ReviewAt
	c.Name, c.Quarter = cur.Name, cur.Quarter
	m.campaigns[c.ID] = c
	m.lines[c.ID] = append([]domain.Commendation(nil), lines...)
	return c, nil
}

func (m *memRecord) ListCommendations(_ context.Context, campaignID uuid.UUID) ([]domain.Commendation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.Commendation(nil), m.lines[campaignID]...), nil
}
