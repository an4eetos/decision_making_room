package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

// staffModeID is the shipped staff appraisal mode: the generals read the map
// rather than a question.
const staffModeID = "staff"

// appraisalTimeout bounds a background appraisal. The consult bounds itself
// by tier (four minutes standard, eight deep); this is the backstop above
// that, so a provider that hangs cannot hold the staff busy for good.
const appraisalTimeout = 10 * time.Minute

// What the staff needs, as interfaces, so an appraisal can be tested without
// a model and a retrieval stack behind it.
type (
	Answerer interface {
		Execute(ctx context.Context, in memusecase.ConsultInput) (memusecase.ConsultResult, error)
	}
	SessionStarter interface {
		StartSessionWith(ctx context.Context, title, content, modeID string, generals []string) (memusecase.ChatSessionDTO, error)
	}
)

// ErrNothingToAppraise is an appraisal asked for on an empty map, or on a
// front with nothing on it.
var ErrNothingToAppraise = errors.New("nothing on the map to appraise; open a front and put an objective on it")

// ErrStaffBusy means an appraisal is already being written. A second click
// should not spend a second set of model calls.
var ErrStaffBusy = errors.New("the staff is already appraising the map; give it a minute")

// Staff has the generals appraise the map on request: where the point of
// main effort is, where to yield ground, what is overextended. It is the one
// place the campaign calls the model because you asked, rather than in the
// background after a turn.
//
// An appraisal runs in the background, on its own context, like extraction
// does: a standard one is one call bounded at four minutes, a deep one a
// council bounded at eight, and a request held open that long with nothing
// on the wire is one a browser or a proxy may drop. So Start returns at
// once, the page polls Status, and an appraisal you asked for and walked
// away from is still there when you come back.
type Staff struct {
	sitreps *Sitreps
	manage  *Manage
	repo    port.AppraisalRepository
	consult Answerer
	chat    SessionStarter
	busy    chan struct{}
	now     func() time.Time

	mu    sync.Mutex
	state StaffStatus
}

// StaffStatus is what the page polls: whether an appraisal is being written,
// since when, and how the last one ended if it failed.
type StaffStatus struct {
	Busy  bool      `json:"busy"`
	Since time.Time `json:"since,omitempty"`
	// Error is the last failure, kept until the next appraisal starts, so
	// a page that was not watching still sees why nothing new arrived.
	Error string `json:"error,omitempty"`
}

func NewStaff(sitreps *Sitreps, manage *Manage, repo port.AppraisalRepository, consult Answerer, chat SessionStarter) *Staff {
	return &Staff{
		sitreps: sitreps, manage: manage, repo: repo, consult: consult, chat: chat,
		busy: make(chan struct{}, 1), now: time.Now,
	}
}

// AppraiseInput narrows an appraisal to one front, and sets its depth.
// Standard seats two generals in one call; deep convenes three as a council.
type AppraiseInput struct {
	FrontID *uuid.UUID
	Tier    string
}

// maxAppraisalObjectives bounds how many objectives the question names, so
// retrieval has the objectives' own words to search by without the question
// becoming the map itself; the map travels in the context.
const maxAppraisalObjectives = 12

// Start checks the request against the map and starts the appraisal in the
// background. It returns at once: ErrStaffBusy if one is already being
// written, ErrNothingToAppraise on an empty map or front, and nil once the
// staff is at work. The result lands in the store; Status says how it went.
func (s *Staff) Start(ctx context.Context, in AppraiseInput) error {
	select {
	case s.busy <- struct{}{}:
	default:
		return ErrStaffBusy
	}

	sitrep, front, err := s.prepare(ctx, in)
	if err != nil {
		<-s.busy
		return err
	}

	s.mu.Lock()
	s.state = StaffStatus{Busy: true, Since: s.now()}
	s.mu.Unlock()

	go func() {
		defer func() { <-s.busy }()
		bg, cancel := context.WithTimeout(context.Background(), appraisalTimeout)
		defer cancel()
		_, err := s.appraise(bg, in, sitrep, front)
		s.mu.Lock()
		s.state = StaffStatus{}
		if err != nil {
			log.Printf("campaign: staff appraisal: %v", err)
			s.state.Error = err.Error()
		}
		s.mu.Unlock()
	}()
	return nil
}

// Status reports whether the staff is at work, and the last failure if any.
func (s *Staff) Status() StaffStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Appraise runs an appraisal and waits for it. The page goes through Start;
// this is for callers that want the result in hand, and for tests.
func (s *Staff) Appraise(ctx context.Context, in AppraiseInput) (domain.Appraisal, error) {
	select {
	case s.busy <- struct{}{}:
	default:
		return domain.Appraisal{}, ErrStaffBusy
	}
	defer func() { <-s.busy }()

	sitrep, front, err := s.prepare(ctx, in)
	if err != nil {
		return domain.Appraisal{}, err
	}
	return s.appraise(ctx, in, sitrep, front)
}

// prepare reads the map and checks there is something to appraise, before
// any model call is spent.
func (s *Staff) prepare(ctx context.Context, in AppraiseInput) (domain.Sitrep, *domain.SitrepFront, error) {
	sitrep, err := s.sitreps.Sitrep(ctx)
	if err != nil {
		return domain.Sitrep{}, nil, err
	}
	if sitrep.Empty {
		return domain.Sitrep{}, nil, ErrNothingToAppraise
	}
	if in.FrontID == nil {
		return sitrep, nil, nil
	}
	for i := range sitrep.Fronts {
		if sitrep.Fronts[i].ID == *in.FrontID {
			front := &sitrep.Fronts[i]
			if len(front.Objectives) == 0 && len(front.Loose) == 0 {
				return domain.Sitrep{}, nil, ErrNothingToAppraise
			}
			return sitrep, front, nil
		}
	}
	return domain.Sitrep{}, nil, fmt.Errorf("front: %w", port.ErrNotFound)
}

// appraise has the generals read the map and stores what they said. The
// sitrep goes into the context through the same reader every answer uses;
// the question names the objectives so retrieval finds the notes behind them.
func (s *Staff) appraise(ctx context.Context, in AppraiseInput, sitrep domain.Sitrep, front *domain.SitrepFront) (domain.Appraisal, error) {
	tier := strings.TrimSpace(in.Tier)
	if tier == "" {
		tier = "standard"
	}
	result, err := s.consult.Execute(ctx, memusecase.ConsultInput{
		Question: appraisalQuestion(sitrep, front),
		Tier:     tier,
		ModeID:   staffModeID,
	})
	if err != nil {
		return domain.Appraisal{}, fmt.Errorf("staff appraisal: %w", err)
	}
	body := strings.TrimSpace(result.Answer)
	if body == "" {
		return domain.Appraisal{}, errors.New("the staff returned an empty appraisal")
	}

	return s.repo.CreateAppraisal(ctx, domain.Appraisal{
		FrontID:  in.FrontID,
		Tier:     result.Tier,
		Generals: result.Generals,
		Body:     body,
	})
}

// appraisalQuestion is what the staff is asked. It names the objectives in
// play so retrieval searches by their words; the map itself reaches the
// prompt through the context, not the question.
func appraisalQuestion(sitrep domain.Sitrep, front *domain.SitrepFront) string {
	var b strings.Builder
	if front != nil {
		fmt.Fprintf(&b, "Appraise the %s front on my campaign map as it stands today, %s. Read the other fronts only for what they take from this one.", front.Name, sitrep.Day)
	} else {
		fmt.Fprintf(&b, "Appraise my campaign map as it stands today, %s.", sitrep.Day)
	}
	b.WriteString(" Where is the point of main effort, where should I yield ground, and what is overextended?")

	var objectives []string
	fronts := sitrep.Fronts
	if front != nil {
		fronts = []domain.SitrepFront{*front}
	}
	for _, f := range fronts {
		for _, o := range f.Objectives {
			if o.Status == domain.StatusActive {
				objectives = append(objectives, o.Text)
			}
		}
	}
	if len(objectives) > maxAppraisalObjectives {
		objectives = objectives[:maxAppraisalObjectives]
	}
	if len(objectives) > 0 {
		b.WriteString(" The objectives in play: ")
		b.WriteString(strings.Join(objectives, "; "))
		b.WriteString(".")
	}
	return b.String()
}

// Appraisals lists what the staff has said, latest first.
func (s *Staff) Appraisals(ctx context.Context, limit int) ([]domain.Appraisal, error) {
	return s.repo.ListAppraisals(ctx, limit)
}

// Open turns an appraisal into a conversation the first time you discuss it,
// and returns the same one after that. The generals who wrote it are seated,
// so arguing back is arguing with them; the mode is left to detection, so a
// follow-up is answered as a question, not as another appraisal.
func (s *Staff) Open(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	a, err := s.repo.GetAppraisal(ctx, id)
	if err != nil {
		return uuid.Nil, err
	}
	if a.SessionID != nil {
		return *a.SessionID, nil
	}

	title := "Staff appraisal · " + a.CreatedAt.Format("Jan 2")
	if a.FrontID != nil && s.manage != nil {
		if f, err := s.manage.repo.GetFront(ctx, *a.FrontID); err == nil {
			title = "Staff appraisal · " + f.Name + " · " + a.CreatedAt.Format("Jan 2")
		}
	}
	session, err := s.chat.StartSessionWith(ctx, title, a.Body, staffModeID, a.Generals)
	if err != nil {
		return uuid.Nil, err
	}
	sessionID, err := uuid.Parse(session.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.repo.AttachAppraisalSession(ctx, id, sessionID); err != nil {
		return uuid.Nil, err
	}
	return sessionID, nil
}
