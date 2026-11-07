// Package usecase manages commitments: creating them by hand, reviewing
// proposals, and resolving them.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
	"github.com/an4eetos/decision-room/internal/commitments/service"
)

type Manage struct {
	repo port.Repository
}

func NewManage(repo port.Repository) *Manage {
	return &Manage{repo: repo}
}

type CreateInput struct {
	Text      string
	DueAt     *time.Time
	Source    domain.Source
	SessionID *uuid.UUID
	MessageID *uuid.UUID
	// Kind and TargetID aim the order at a campaign item; see Commitment.
	Kind     domain.Kind
	TargetID *uuid.UUID
	// ModeID is the mode of the turn it came from, so a proposal drawn up by
	// an interrogation does not claim you said it.
	ModeID string
}

// Create adds a commitment you made explicitly. It goes straight to open: you
// typed it, so there is nothing to confirm.
//
// A duplicate of something already live returns the existing row rather than an
// error — "track this" twice on the same thing should be idempotent.
func (u *Manage) Create(ctx context.Context, in CreateInput) (domain.Commitment, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return domain.Commitment{}, fmt.Errorf("text is required")
	}
	if len([]rune(text)) > 240 {
		return domain.Commitment{}, fmt.Errorf("keep it under 240 characters; a commitment is one thing")
	}

	source := in.Source
	if source == "" {
		source = domain.SourceManual
	}

	c, err := u.repo.Create(ctx, domain.Commitment{
		Text:        text,
		Status:      domain.StatusOpen,
		DueAt:       in.DueAt,
		Source:      source,
		SessionID:   in.SessionID,
		MessageID:   in.MessageID,
		Confidence:  1,
		Fingerprint: service.Fingerprint(text),
		Kind:        in.Kind,
		TargetID:    in.TargetID,
	})
	if errors.Is(err, port.ErrDuplicate) {
		return u.findLive(ctx, service.Fingerprint(text))
	}
	return c, err
}

func (u *Manage) findLive(ctx context.Context, fingerprint string) (domain.Commitment, error) {
	live, err := u.repo.List(ctx, []domain.Status{domain.StatusProposed, domain.StatusOpen}, 500)
	if err != nil {
		return domain.Commitment{}, err
	}
	for _, c := range live {
		if c.Fingerprint == fingerprint {
			// Tracking something that was only proposed confirms it.
			if c.Status == domain.StatusProposed {
				return u.repo.SetStatus(ctx, c.ID, domain.StatusOpen)
			}
			return c, nil
		}
	}
	return domain.Commitment{}, port.ErrDuplicate
}

// List returns commitments. An empty filter means the working set: proposals
// waiting for you, and open ones.
func (u *Manage) List(ctx context.Context, statuses []domain.Status) ([]domain.Commitment, error) {
	if len(statuses) == 0 {
		statuses = []domain.Status{domain.StatusProposed, domain.StatusOpen}
	}
	return u.repo.List(ctx, statuses, 200)
}

func (u *Manage) Get(ctx context.Context, id uuid.UUID) (domain.Commitment, error) {
	return u.repo.Get(ctx, id)
}

// allowed lists the transitions that make sense. Mostly this stops a proposal
// being marked done without first being accepted, which would put something you
// never agreed to into your record of what you finished.
var allowed = map[domain.Status][]domain.Status{
	domain.StatusProposed: {domain.StatusOpen, domain.StatusDropped},
	domain.StatusOpen:     {domain.StatusDone, domain.StatusDropped, domain.StatusStale},
	domain.StatusStale:    {domain.StatusOpen, domain.StatusDone, domain.StatusDropped},
	domain.StatusDone:     {domain.StatusOpen},
	domain.StatusDropped:  {domain.StatusOpen},
}

var ErrBadTransition = errors.New("that status change does not apply")

func (u *Manage) SetStatus(ctx context.Context, id uuid.UUID, to domain.Status) (domain.Commitment, error) {
	if !to.Valid() {
		return domain.Commitment{}, fmt.Errorf("unknown status %q", to)
	}

	current, err := u.repo.Get(ctx, id)
	if err != nil {
		return domain.Commitment{}, err
	}
	if current.Status == to {
		return current, nil
	}

	ok := false
	for _, next := range allowed[current.Status] {
		if next == to {
			ok = true
			break
		}
	}
	if !ok {
		return domain.Commitment{}, fmt.Errorf("%w: %s to %s", ErrBadTransition, current.Status, to)
	}

	return u.repo.SetStatus(ctx, id, to)
}

// SetTarget aims an order at a campaign item, or with nil at nothing. The caller
// has checked the target exists; commitments do not know what the map holds.
func (u *Manage) SetTarget(ctx context.Context, id uuid.UUID, target *uuid.UUID, kind domain.Kind) (domain.Commitment, error) {
	if kind != "" && !kind.Valid() {
		return domain.Commitment{}, fmt.Errorf("unknown kind %q", kind)
	}
	return u.repo.SetTarget(ctx, id, target, kind)
}

// ByTargets lists the orders aimed at the given campaign items.
func (u *Manage) ByTargets(ctx context.Context, targets []uuid.UUID) ([]domain.Commitment, error) {
	return u.repo.ListByTargets(ctx, targets)
}

// Propose adds an order the room drew up for you — a probe or an order from an
// interrogation's position. It waits as a proposal like anything extracted;
// already live is not an error.
func (u *Manage) Propose(ctx context.Context, in CreateInput, confidence float64) (domain.Commitment, error) {
	text := strings.TrimSpace(in.Text)
	if text == "" || len([]rune(text)) > 240 {
		return domain.Commitment{}, fmt.Errorf("a proposed order needs 1 to 240 characters")
	}
	source := in.Source
	if source == "" {
		source = domain.SourceChat
	}
	c, err := u.repo.Create(ctx, domain.Commitment{
		Text:        text,
		Status:      domain.StatusProposed,
		DueAt:       in.DueAt,
		Source:      source,
		SessionID:   in.SessionID,
		MessageID:   in.MessageID,
		ModeID:      in.ModeID,
		Confidence:  confidence,
		Fingerprint: service.Fingerprint(text),
		Kind:        in.Kind,
		TargetID:    in.TargetID,
	})
	return c, err
}

// Edit changes the wording or due date. Editing a proposal is taken as
// accepting it — you would not reword something you meant to drop.
func (u *Manage) Edit(ctx context.Context, id uuid.UUID, text string, due *time.Time) (domain.Commitment, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.Commitment{}, fmt.Errorf("text is required")
	}

	current, err := u.repo.Get(ctx, id)
	if err != nil {
		return domain.Commitment{}, err
	}

	updated, err := u.repo.UpdateText(ctx, id, text, service.Fingerprint(text), due)
	if err != nil {
		return domain.Commitment{}, err
	}
	if current.Status == domain.StatusProposed {
		return u.repo.SetStatus(ctx, id, domain.StatusOpen)
	}
	return updated, nil
}
