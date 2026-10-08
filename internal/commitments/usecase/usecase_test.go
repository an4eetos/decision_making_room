package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

type stubLLM struct {
	answer string
	err    error
	calls  int
}

func (s *stubLLM) Chat(context.Context, []memport.Message) (string, error) {
	s.calls++
	return s.answer, s.err
}

func TestManualCommitmentsAreOpenImmediately(t *testing.T) {
	t.Parallel()

	c, err := NewManage(newMemRepo()).Create(context.Background(), CreateInput{Text: "call the landlord"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != domain.StatusOpen {
		t.Fatalf("status = %q; you typed it, so it needs no confirming", c.Status)
	}
}

// "Track this" twice on the same thing must be idempotent, not an error.
func TestCreatingADuplicateReturnsTheExistingOne(t *testing.T) {
	t.Parallel()

	m := NewManage(newMemRepo())
	first, _ := m.Create(context.Background(), CreateInput{Text: "Ship the MVP by Friday"})
	second, err := m.Create(context.Background(), CreateInput{Text: "ship MVP by friday"})
	if err != nil {
		t.Fatalf("duplicate should not error: %v", err)
	}
	if second.ID != first.ID {
		t.Fatal("expected the existing commitment back")
	}
}

// Tracking something that was only proposed confirms the proposal.
func TestTrackingAProposalConfirmsIt(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	proposal, _ := repo.Create(context.Background(), domain.Commitment{
		Text: "ship the MVP", Status: domain.StatusProposed, Fingerprint: fp("ship the MVP"),
	})

	got, err := NewManage(repo).Create(context.Background(), CreateInput{Text: "ship the MVP"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != proposal.ID || got.Status != domain.StatusOpen {
		t.Fatalf("expected the proposal confirmed, got %+v", got)
	}
}

// A proposal cannot jump to done: that would put something you never agreed to
// into the record of what you finished.
func TestProposalCannotBeMarkedDoneDirectly(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	p, _ := repo.Create(context.Background(), domain.Commitment{
		Text: "x thing", Status: domain.StatusProposed, Fingerprint: fp("x thing"),
	})

	_, err := NewManage(repo).SetStatus(context.Background(), p.ID, domain.StatusDone)
	if !errors.Is(err, ErrBadTransition) {
		t.Fatalf("err = %v, want ErrBadTransition", err)
	}
}

func TestKeepAndDropProposals(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	m := NewManage(repo)
	keep, _ := repo.Create(context.Background(), domain.Commitment{Text: "keep me", Status: domain.StatusProposed, Fingerprint: fp("keep me")})
	drop, _ := repo.Create(context.Background(), domain.Commitment{Text: "drop me", Status: domain.StatusProposed, Fingerprint: fp("drop me")})

	if c, err := m.SetStatus(context.Background(), keep.ID, domain.StatusOpen); err != nil || c.Status != domain.StatusOpen {
		t.Fatalf("keep: %v %v", c.Status, err)
	}
	if c, err := m.SetStatus(context.Background(), drop.ID, domain.StatusDropped); err != nil || c.Status != domain.StatusDropped {
		t.Fatalf("drop: %v %v", c.Status, err)
	}
}

// Rewording a proposal means you meant to keep it.
func TestEditingAProposalAcceptsIt(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	p, _ := repo.Create(context.Background(), domain.Commitment{Text: "ship", Status: domain.StatusProposed, Fingerprint: fp("ship")})

	c, err := NewManage(repo).Edit(context.Background(), p.ID, "ship the relocation UI", nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != domain.StatusOpen || c.Text != "ship the relocation UI" {
		t.Fatalf("got %+v", c)
	}
}

func TestSweepMarksOnlyUntouchedOpenCommitments(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	old, _ := repo.Create(context.Background(), domain.Commitment{Text: "old", Status: domain.StatusOpen, Fingerprint: fp("old")})
	fresh, _ := repo.Create(context.Background(), domain.Commitment{Text: "fresh", Status: domain.StatusOpen, Fingerprint: fp("fresh")})
	proposal, _ := repo.Create(context.Background(), domain.Commitment{Text: "prop", Status: domain.StatusProposed, Fingerprint: fp("prop")})

	longAgo := time.Now().Add(-30 * 24 * time.Hour)
	repo.touch(old.ID, longAgo)
	repo.touch(proposal.ID, longAgo)

	stale, err := NewSweep(repo, 14*24*time.Hour).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].ID != old.ID {
		t.Fatalf("expected only the old open one to go stale, got %d", len(stale))
	}
	if c, _ := repo.Get(context.Background(), fresh.ID); c.Status != domain.StatusOpen {
		t.Fatal("a recently touched commitment went stale")
	}
	// Proposals are not yours yet, so they cannot go stale on you.
	if c, _ := repo.Get(context.Background(), proposal.ID); c.Status != domain.StatusProposed {
		t.Fatal("a proposal went stale")
	}
}

func turn(user string) memport.Turn {
	return memport.Turn{SessionID: uuid.New(), MessageID: uuid.New(), UserText: user, AssistantText: "ok", ModeID: "day_plan"}
}

func TestExtractionCreatesProposalsNotOpenItems(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	llm := &stubLLM{answer: `[{"text":"ship the MVP","due":null,"confidence":0.9}]`}

	got, err := NewExtract(llm, repo, true).Run(context.Background(), turn("I'll ship the MVP"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Status != domain.StatusProposed {
		t.Fatalf("got %+v; extraction must only ever propose", got)
	}
	if got[0].Source != domain.SourceChat || got[0].SessionID == nil {
		t.Fatal("proposal should record where it came from")
	}
}

// Saying the same thing twice must not produce two proposals.
func TestExtractionSkipsAlreadyTrackedCommitments(t *testing.T) {
	t.Parallel()

	repo := newMemRepo()
	_, _ = NewManage(repo).Create(context.Background(), CreateInput{Text: "ship the MVP"})

	llm := &stubLLM{answer: `[{"text":"Ship the MVP","due":null,"confidence":0.9}]`}
	got, err := NewExtract(llm, repo, true).Run(context.Background(), turn("I'll ship the MVP"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d new proposals for something already tracked", len(got))
	}
}

// The prefilter is what keeps extraction from costing a model call on every
// turn. A question should never reach the model.
func TestObserveTurnSkipsTheModelWithoutCommitmentLanguage(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{answer: "[]"}
	e := NewExtract(llm, newMemRepo(), true)

	e.ObserveTurn(turn("What did I decide about the database?"))
	time.Sleep(50 * time.Millisecond)

	if llm.calls != 0 {
		t.Fatalf("model called %d times for a turn with no commitment in it", llm.calls)
	}
}

func TestObserveTurnDisabledDoesNothing(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{answer: "[]"}
	NewExtract(llm, newMemRepo(), false).ObserveTurn(turn("I'll ship it by Friday"))
	time.Sleep(50 * time.Millisecond)

	if llm.calls != 0 {
		t.Fatal("extraction ran while disabled")
	}
}

func TestExtractionFailureIsReturnedNotPanicked(t *testing.T) {
	t.Parallel()

	llm := &stubLLM{err: errors.New("429 quota")}
	if _, err := NewExtract(llm, newMemRepo(), true).Run(context.Background(), turn("I'll do it")); err == nil {
		t.Fatal("expected the model error to surface")
	}
}

type fixedObjectives []port.ObjectiveRef

func (f fixedObjectives) ActiveObjectives(context.Context) ([]port.ObjectiveRef, error) {
	return f, nil
}

// With a campaign map wired in, an extracted commitment is linked to the
// objective it serves; a number the model invents links nothing.
func TestExtractionLinksTheObjectiveItServes(t *testing.T) {
	t.Parallel()

	lisbon := uuid.New()
	objectives := fixedObjectives{{ID: uuid.New(), Text: "Get fit"}, {ID: lisbon, Text: "Move to Lisbon"}}
	llm := &stubLLM{answer: `[{"text":"book the visa appointment","due":null,"confidence":0.9,"serves":2},
		{"text":"call mum","due":null,"confidence":0.9,"serves":7}]`}

	got, err := NewExtract(llm, newMemRepo(), true).WithObjectives(objectives).
		Run(context.Background(), turn("I'll book the visa appointment and call mum"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d proposals, want 2", len(got))
	}
	if got[0].TargetID == nil || *got[0].TargetID != lisbon {
		t.Fatalf("first commitment should serve Lisbon: %+v", got[0])
	}
	if got[1].TargetID != nil {
		t.Fatalf("an out-of-range number links nothing: %+v", got[1])
	}
}
