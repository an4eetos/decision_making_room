// Package usecase runs the campaign: fronts you create, items you keep or drop,
// orders aimed at them, and the fog lifted by reconnaissance.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/service"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comport "github.com/an4eetos/decision-room/internal/commitments/port"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
)

// ErrBadTransition is a status change that does not apply to the item.
var ErrBadTransition = errors.New("that change does not apply")

// maxFrontRunes keeps a front a name, not a description.
const maxFrontRunes = 40

// resolvedWindow is how long a taken objective or lifted unknown stays on the
// map after it is resolved: long enough to see what moved, short enough that
// the map is about now.
const resolvedWindow = 30 * 24 * time.Hour

type Manage struct {
	repo   port.Repository
	orders *comusecase.Manage
	now    func() time.Time
}

func NewManage(repo port.Repository, orders *comusecase.Manage) *Manage {
	return &Manage{repo: repo, orders: orders, now: time.Now}
}

// ---------- Fronts ----------

func (u *Manage) CreateFront(ctx context.Context, name string) (domain.Front, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Front{}, fmt.Errorf("a front needs a name")
	}
	if len([]rune(name)) > maxFrontRunes {
		return domain.Front{}, fmt.Errorf("keep a front's name under %d characters; it is an area, not a plan", maxFrontRunes)
	}
	return u.repo.CreateFront(ctx, name)
}

// AddStarters creates the starter fronts that do not exist yet. It is what the
// empty campaign offers, so pressing it twice changes nothing.
func (u *Manage) AddStarters(ctx context.Context) ([]domain.Front, error) {
	for _, name := range domain.StarterFronts {
		if _, err := u.repo.CreateFront(ctx, name); err != nil && !errors.Is(err, port.ErrDuplicate) {
			return nil, err
		}
	}
	return u.repo.ListFronts(ctx, true)
}

// UpdateFront renames a front, or withdraws or restores it. A withdrawn front
// keeps its items: closing an area of life is an orderly withdrawal, and what
// was fought for there stays on the record.
func (u *Manage) UpdateFront(ctx context.Context, id uuid.UUID, name *string, status *domain.FrontStatus) (domain.Front, error) {
	f, err := u.repo.GetFront(ctx, id)
	if err != nil {
		return domain.Front{}, err
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || len([]rune(n)) > maxFrontRunes {
			return domain.Front{}, fmt.Errorf("a front's name needs 1 to %d characters", maxFrontRunes)
		}
		f.Name = n
	}
	if status != nil {
		if *status != domain.FrontActive && *status != domain.FrontWithdrawn {
			return domain.Front{}, fmt.Errorf("unknown front status %q", *status)
		}
		f.Status = *status
	}
	return u.repo.UpdateFront(ctx, f)
}

// ---------- Items ----------

// CreateInput is an item you add by hand. It goes straight onto the map: you
// typed it, so there is nothing to confirm — including the strength, which is
// confirmed because you set it.
type CreateInput struct {
	Type        domain.Type
	Text        string
	FrontID     *uuid.UUID
	ObjectiveID *uuid.UUID
	Kind        domain.Kind
	Strength    int
	DueAt       *time.Time
}

func (u *Manage) CreateItem(ctx context.Context, in CreateInput) (domain.Item, error) {
	if !in.Type.Valid() {
		return domain.Item{}, fmt.Errorf("unknown type %q", in.Type)
	}
	text, err := cleanText(in.Text)
	if err != nil {
		return domain.Item{}, err
	}

	item := domain.Item{
		Type:        in.Type,
		Text:        text,
		Status:      domain.StatusActive,
		Source:      domain.SourceManual,
		Confidence:  1,
		Fingerprint: service.Fingerprint(text),
		DueAt:       in.DueAt,
	}
	if err := u.place(ctx, &item, in.FrontID, in.ObjectiveID); err != nil {
		return domain.Item{}, err
	}
	if err := setObstacleFields(&item, in.Kind, in.Strength, true); err != nil {
		return domain.Item{}, err
	}
	return u.repo.CreateItem(ctx, item)
}

// Patch is a partial edit. Nil fields are left alone; ClearFront and
// ClearObjective unassign.
type Patch struct {
	Text           *string
	FrontID        *uuid.UUID
	ClearFront     bool
	ObjectiveID    *uuid.UUID
	ClearObjective bool
	Kind           *domain.Kind
	// Strength sets the strength and confirms it: a strength you set is no
	// longer an estimate.
	Strength *int
	// ConfirmStrength confirms the estimate as it stands.
	ConfirmStrength bool
	Status          *domain.Status
	DueAt           *time.Time
	ClearDue        bool
}

// allowed are the transitions that make sense. Mostly they stop a proposal
// being marked taken or cleared without first being kept, which would put
// something you never agreed to into your record of what moved.
var allowed = map[domain.Status][]domain.Status{
	domain.StatusProposed:  {domain.StatusActive, domain.StatusDropped},
	domain.StatusActive:    {domain.StatusResolved, domain.StatusWithdrawn, domain.StatusDropped},
	domain.StatusResolved:  {domain.StatusActive},
	domain.StatusWithdrawn: {domain.StatusActive},
	domain.StatusDropped:   {domain.StatusActive},
}

// Update edits an item and moves it through its life. Editing a proposal keeps
// it — you would not correct something you meant to drop — unless the same
// request drops it.
func (u *Manage) Update(ctx context.Context, id uuid.UUID, p Patch) (domain.Item, error) {
	item, err := u.repo.GetItem(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}
	edited := false

	if p.Text != nil {
		text, err := cleanText(*p.Text)
		if err != nil {
			return domain.Item{}, err
		}
		item.Text, item.Fingerprint = text, service.Fingerprint(text)
		edited = true
	}

	front, objective := item.FrontID, item.ObjectiveID
	if p.FrontID != nil {
		front, edited = p.FrontID, true
	}
	if p.ClearFront {
		front, edited = nil, true
	}
	if p.ObjectiveID != nil {
		objective, edited = p.ObjectiveID, true
	}
	if p.ClearObjective {
		objective, edited = nil, true
	}
	if edited {
		if err := u.place(ctx, &item, front, objective); err != nil {
			return domain.Item{}, err
		}
	}

	if p.Kind != nil || p.Strength != nil || p.ConfirmStrength {
		if item.Type != domain.TypeObstacle {
			return domain.Item{}, fmt.Errorf("only an obstacle has a kind and a strength")
		}
		kind, strength, confirmed := item.Kind, item.Strength, item.StrengthConfirmed
		if p.Kind != nil {
			kind = *p.Kind
		}
		if p.Strength != nil {
			strength, confirmed = *p.Strength, true
		}
		if p.ConfirmStrength {
			confirmed = true
		}
		if err := setObstacleFields(&item, kind, strength, confirmed); err != nil {
			return domain.Item{}, err
		}
		edited = true
	}

	if p.DueAt != nil || p.ClearDue {
		if item.Type != domain.TypeObjective {
			return domain.Item{}, fmt.Errorf("only an objective has a date")
		}
		item.DueAt = p.DueAt
		if p.ClearDue {
			item.DueAt = nil
		}
		edited = true
	}

	to := item.Status
	if p.Status != nil {
		to = *p.Status
	} else if edited && item.Status == domain.StatusProposed {
		to = domain.StatusActive
	}
	if to != item.Status {
		if err := transition(item, to); err != nil {
			return domain.Item{}, err
		}
		item.Status = to
	}

	return u.repo.UpdateItem(ctx, item)
}

// Lift clears an unknown with what reconnaissance found. The answer is the
// point: fog lifted without one has only been forgotten.
func (u *Manage) Lift(ctx context.Context, id uuid.UUID, answer string) (domain.Item, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return domain.Item{}, fmt.Errorf("say what reconnaissance found; that is what lifts the fog")
	}
	item, err := u.repo.GetItem(ctx, id)
	if err != nil {
		return domain.Item{}, err
	}
	if item.Type != domain.TypeUnknown {
		return domain.Item{}, fmt.Errorf("only an unknown is lifted")
	}
	if item.Status == domain.StatusProposed {
		// Answering a proposed unknown keeps it, then lifts it.
		item.Status = domain.StatusActive
	}
	if err := transition(item, domain.StatusResolved); err != nil {
		return domain.Item{}, err
	}
	item.Status, item.Answer = domain.StatusResolved, answer
	return u.repo.UpdateItem(ctx, item)
}

func transition(item domain.Item, to domain.Status) error {
	if !to.Valid() {
		return fmt.Errorf("unknown status %q", to)
	}
	if to == domain.StatusWithdrawn && item.Type != domain.TypeObjective {
		return fmt.Errorf("%w: only an objective is withdrawn", ErrBadTransition)
	}
	if !slices.Contains(allowed[item.Status], to) {
		return fmt.Errorf("%w: %s to %s", ErrBadTransition, item.Status, to)
	}
	return nil
}

// place checks and sets the front and the objective an item sits under. An
// objective cannot sit under another objective; an obstacle or unknown inherits
// its objective's front when it has none of its own.
func (u *Manage) place(ctx context.Context, item *domain.Item, frontID, objectiveID *uuid.UUID) error {
	if frontID != nil {
		front, err := u.repo.GetFront(ctx, *frontID)
		if err != nil {
			return fmt.Errorf("front: %w", err)
		}
		if front.Status != domain.FrontActive {
			return fmt.Errorf("that front is withdrawn; restore it first")
		}
	}
	if objectiveID != nil {
		if item.Type == domain.TypeObjective {
			return fmt.Errorf("an objective does not sit under another objective")
		}
		objective, err := u.repo.GetItem(ctx, *objectiveID)
		if err != nil {
			return fmt.Errorf("objective: %w", err)
		}
		if objective.Type != domain.TypeObjective {
			return fmt.Errorf("an obstacle or unknown bears on an objective, not on a %s", objective.Type)
		}
		if frontID == nil {
			frontID = objective.FrontID
		}
	}
	item.FrontID, item.ObjectiveID = frontID, objectiveID
	return nil
}

func setObstacleFields(item *domain.Item, kind domain.Kind, strength int, confirmed bool) error {
	if item.Type != domain.TypeObstacle {
		if kind != "" || strength != 0 {
			return fmt.Errorf("only an obstacle has a kind and a strength")
		}
		return nil
	}
	if kind != "" && !kind.Valid() {
		return fmt.Errorf("unknown kind %q", kind)
	}
	if strength != 0 && !domain.ValidStrength(strength) {
		return fmt.Errorf("strength runs from 1 (outpost) to 3 (fortress)")
	}
	item.Kind, item.Strength = kind, strength
	item.StrengthConfirmed = confirmed && strength != 0
	return nil
}

func cleanText(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	if len([]rune(text)) > 200 {
		return "", fmt.Errorf("keep it under 200 characters; an item is one thing")
	}
	return text, nil
}

// ---------- The map ----------

// Map is the campaign as it stands: fronts, the items on them, and the orders
// aimed at each.
type Map struct {
	Fronts []domain.Front
	Items  []domain.Item
	// Orders are commitments aimed at the items above.
	Orders []comdomain.Commitment
}

// Map returns the live campaign plus what was resolved recently.
func (u *Manage) Map(ctx context.Context) (Map, error) {
	fronts, err := u.repo.ListFronts(ctx, false)
	if err != nil {
		return Map{}, err
	}
	all, err := u.repo.ListItems(ctx, nil, 1000)
	if err != nil {
		return Map{}, err
	}

	cutoff := u.now().Add(-resolvedWindow)
	var items []domain.Item
	var ids []uuid.UUID
	for _, it := range all {
		if it.Status == domain.StatusDropped {
			continue
		}
		if !it.Status.Live() && it.ResolvedAt != nil && it.ResolvedAt.Before(cutoff) {
			continue
		}
		items = append(items, it)
		ids = append(ids, it.ID)
	}

	var orders []comdomain.Commitment
	if u.orders != nil {
		if orders, err = u.orders.ByTargets(ctx, ids); err != nil {
			return Map{}, err
		}
	}
	return Map{Fronts: fronts, Items: items, Orders: orders}, nil
}

// Proposals are the items waiting for you, for the chat rail.
func (u *Manage) Proposals(ctx context.Context) ([]domain.Item, error) {
	return u.repo.ListItems(ctx, []domain.Status{domain.StatusProposed}, 50)
}

// Fronts lists the fronts in display order.
func (u *Manage) Fronts(ctx context.Context) ([]domain.Front, error) {
	return u.repo.ListFronts(ctx, false)
}

// ---------- Orders ----------

// Aim points an open loop at a campaign item, or with nil at nothing. An order
// aimed at an unknown is reconnaissance.
func (u *Manage) Aim(ctx context.Context, commitmentID uuid.UUID, target *uuid.UUID) (comdomain.Commitment, error) {
	if target == nil {
		return u.orders.SetTarget(ctx, commitmentID, nil, comdomain.KindOrder)
	}
	item, err := u.repo.GetItem(ctx, *target)
	if err != nil {
		return comdomain.Commitment{}, err
	}
	if !item.Status.Live() {
		return comdomain.Commitment{}, fmt.Errorf("that item is off the map; aim at something live")
	}
	kind := comdomain.KindOrder
	if item.Type == domain.TypeUnknown {
		kind = comdomain.KindRecon
	}
	return u.orders.SetTarget(ctx, commitmentID, target, kind)
}

// SendRecon creates a recon order against an unknown: the open loop that, when
// done, asks what it found. Sending it keeps a proposed unknown.
func (u *Manage) SendRecon(ctx context.Context, unknownID uuid.UUID) (comdomain.Commitment, error) {
	item, err := u.repo.GetItem(ctx, unknownID)
	if err != nil {
		return comdomain.Commitment{}, err
	}
	if item.Type != domain.TypeUnknown {
		return comdomain.Commitment{}, fmt.Errorf("reconnaissance is sent against an unknown")
	}
	if !item.Status.Live() {
		return comdomain.Commitment{}, fmt.Errorf("that unknown is already off the map")
	}
	if item.Status == domain.StatusProposed {
		item.Status = domain.StatusActive
		if item, err = u.repo.UpdateItem(ctx, item); err != nil {
			return comdomain.Commitment{}, err
		}
	}

	id := item.ID
	return u.orders.Create(ctx, comusecase.CreateInput{
		Text:     "Find out: " + strings.TrimSuffix(item.Text, "?") + "?",
		Kind:     comdomain.KindRecon,
		TargetID: &id,
	})
}

// ActiveObjectives implements the commitments ObjectiveSource, so a commitment
// extracted from conversation can say which objective it serves.
func (u *Manage) ActiveObjectives(ctx context.Context) ([]comport.ObjectiveRef, error) {
	items, err := u.repo.ListItems(ctx, []domain.Status{domain.StatusActive}, 200)
	if err != nil {
		return nil, err
	}
	var out []comport.ObjectiveRef
	for _, it := range items {
		if it.Type == domain.TypeObjective {
			out = append(out, comport.ObjectiveRef{ID: it.ID, Text: it.Text})
		}
	}
	return out, nil
}
