// Package usecase produces check-ins and turns them into conversations.
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

	"github.com/an4eetos/decision-room/internal/checkin/domain"
	"github.com/an4eetos/decision-room/internal/checkin/port"
	"github.com/an4eetos/decision-room/internal/checkin/service"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

// The three things a check-in needs, as interfaces so the generator can be
// tested without a model, a database and a retrieval stack behind it.
type (
	Answerer interface {
		Execute(ctx context.Context, in memusecase.ConsultInput) (memusecase.ConsultResult, error)
	}
	SessionStarter interface {
		StartSession(ctx context.Context, title, content, modeID string) (memusecase.ChatSessionDTO, error)
	}
	CommitmentLister interface {
		List(ctx context.Context, statuses []comdomain.Status) ([]comdomain.Commitment, error)
	}
)

type Generate struct {
	consult     Answerer
	chat        SessionStarter
	commitments CommitmentLister
	repo        port.Repository
	notifier    *Notifier
	loc         *time.Location
	now         func() time.Time

	// mu serialises generation. The scheduler ticks from one goroutine, but
	// "check in now" arrives from HTTP; without this both could spend a model
	// call on the same slot, and only one row would survive the unique index.
	mu sync.Mutex
}

func NewGenerate(
	consult Answerer,
	chat SessionStarter,
	commitments CommitmentLister,
	repo port.Repository,
	notifier *Notifier,
	loc *time.Location,
) *Generate {
	if loc == nil {
		loc = time.Local
	}
	return &Generate{
		consult: consult, chat: chat, commitments: commitments,
		repo: repo, notifier: notifier, loc: loc, now: time.Now,
	}
}

// intents is what each shape of check-in is for. The model is told this rather
// than handed the mode's full output template, because a check-in is a short
// message that ends in a question, not a plan.
var intents = map[string]string{
	"day_plan":    "Help me decide what today is actually for. If something has been carried for days, name it.",
	"block_start": "Check whether I'm on what I meant to be on this morning, without lecturing.",
	"debrief":     "Ask what closed today and what is carrying over. If something I committed to was due, ask about it by name.",
}

var titles = map[string]string{
	"day_plan":    "Morning check-in",
	"block_start": "Midday check-in",
	"debrief":     "Evening check-in",
}

// Scheduled writes the check-in for a slot. It costs one model call, at the
// quick tier. ErrAlreadyFired means today's already exists, which callers treat
// as success.
func (g *Generate) Scheduled(ctx context.Context, slot service.Slot, localDate time.Time) (domain.CheckIn, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Checked under the lock, before spending a model call.
	if exists, err := g.repo.Exists(ctx, slot.Name, localDate); err != nil {
		return domain.CheckIn{}, err
	} else if exists {
		return domain.CheckIn{}, port.ErrAlreadyFired
	}

	body, err := g.write(ctx, slot.ModeID)
	if err != nil {
		return domain.CheckIn{}, err
	}

	title := titles[slot.ModeID]
	if title == "" {
		title = strings.ToUpper(slot.Name[:1]) + slot.Name[1:] + " check-in"
	}

	return g.store(ctx, domain.CheckIn{
		Slot: slot.Name, Kind: domain.KindScheduled, LocalDate: localDate,
		ModeID: slot.ModeID, Title: title, Body: body,
	})
}

// Now is "check in with me now", from the button. It works whether or not
// scheduled check-ins are enabled — you asked — and picks its shape from the
// time of day.
func (g *Generate) Now(ctx context.Context) (domain.CheckIn, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	local := g.now().In(g.loc)
	mode := service.ModeForHour(local.Hour())

	body, err := g.write(ctx, mode)
	if err != nil {
		return domain.CheckIn{}, err
	}

	// Minute-resolution slot: a double-click is one check-in, not two model
	// calls' worth.
	return g.store(ctx, domain.CheckIn{
		Slot: "now-" + local.Format("1504"), Kind: domain.KindScheduled,
		LocalDate: service.LocalDate(local), ModeID: mode,
		Title: "Check-in", Body: body,
	})
}

func (g *Generate) write(ctx context.Context, modeID string) (string, error) {
	open, err := g.commitments.List(ctx, []comdomain.Status{comdomain.StatusOpen})
	if err != nil {
		return "", fmt.Errorf("load commitments: %w", err)
	}

	local := g.now().In(g.loc)
	question := g.question(modeID, local, open)

	result, err := g.consult.Execute(ctx, memusecase.ConsultInput{
		Question: question,
		Tier:     "quick",
		ModeID:   modeID,
		Plain:    true,
	})
	if err != nil {
		return "", fmt.Errorf("write check-in: %w", err)
	}

	body := strings.TrimSpace(result.Answer)
	if body == "" {
		return "", errors.New("the model returned an empty check-in")
	}
	return body, nil
}

func (g *Generate) question(modeID string, local time.Time, open []comdomain.Commitment) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Write me a short check-in. It is %s, %s.\n\n",
		local.Weekday(), local.Format("15:04"))

	intent := intents[modeID]
	if intent == "" {
		intent = intents["block_start"]
	}
	b.WriteString(intent)
	b.WriteString("\n\n")

	if len(open) > 0 {
		b.WriteString("What I've committed to and not closed:\n")
		for i, c := range open {
			if i == 6 {
				break
			}
			fmt.Fprintf(&b, "- %s%s\n", c.Text, dueNote(c, local))
		}
		b.WriteString("\n")
	}

	b.WriteString("Under 90 words. Refer to at least one specific thing — from the list " +
		"above or from my notes — not generic advice. No headings, no bullet points. " +
		"End with exactly one question I can answer in a sentence.")
	return b.String()
}

func dueNote(c comdomain.Commitment, now time.Time) string {
	if c.DueAt == nil {
		return ""
	}
	due := c.DueAt.In(now.Location())
	switch {
	case due.Before(now):
		return fmt.Sprintf(" (was due %s — overdue)", due.Format("Mon Jan 2"))
	case service.LocalDate(due).Equal(service.LocalDate(now)):
		return " (due today)"
	default:
		return fmt.Sprintf(" (due %s)", due.Format("Mon Jan 2"))
	}
}

// Idle nudges when you have gone quiet. No model call: the useful content is
// the specific overdue thing, which the database already knows.
func (g *Generate) Idle(ctx context.Context, lastActivity time.Time) (domain.CheckIn, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	local := g.now().In(g.loc)
	date := service.LocalDate(local)

	if exists, err := g.repo.Exists(ctx, "idle", date); err != nil || exists {
		if err != nil {
			return domain.CheckIn{}, err
		}
		return domain.CheckIn{}, port.ErrAlreadyFired
	}

	open, err := g.commitments.List(ctx, []comdomain.Status{comdomain.StatusOpen})
	if err != nil {
		return domain.CheckIn{}, err
	}

	hours := int(local.Sub(lastActivity).Hours())
	var b strings.Builder
	fmt.Fprintf(&b, "It's been %d hours since you last wrote anything. ", hours)

	if urgent := mostUrgent(open, local); urgent != nil {
		fmt.Fprintf(&b, "You said you'd **%s**%s — did that happen?", urgent.Text, dueNote(*urgent, local))
	} else {
		b.WriteString("What happened since then?")
	}

	return g.store(ctx, domain.CheckIn{
		Slot: "idle", Kind: domain.KindNudge, LocalDate: date,
		ModeID: "debrief", Title: "Quiet for a while", Body: b.String(),
	})
}

// Stale nudges once, at the moment commitments go stale. Firing from the
// transition names each item exactly once; re-reading the stale list daily
// would nag about the same thing forever.
func (g *Generate) Stale(ctx context.Context, items []comdomain.Commitment) (domain.CheckIn, error) {
	if len(items) == 0 {
		return domain.CheckIn{}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	local := g.now().In(g.loc)

	var b strings.Builder
	if len(items) == 1 {
		fmt.Fprintf(&b, "Two weeks ago you said you'd **%s**. It hasn't moved since. ", items[0].Text)
		b.WriteString("Is it still something you mean to do, or is it time to drop it?")
	} else {
		fmt.Fprintf(&b, "%d things you committed to haven't moved in two weeks:\n\n", len(items))
		for i, c := range items {
			if i == 5 {
				fmt.Fprintf(&b, "- and %d more\n", len(items)-5)
				break
			}
			fmt.Fprintf(&b, "- %s\n", c.Text)
		}
		b.WriteString("\nWhich of these do you still mean? Dropping one on purpose is better than carrying it.")
	}

	// Keyed on the first item so each batch is one check-in, and a second
	// sweep the same day marking different items still gets its own.
	return g.store(ctx, domain.CheckIn{
		Slot: "stale-" + items[0].ID.String()[:8], Kind: domain.KindNudge,
		LocalDate: service.LocalDate(local), ModeID: "overloaded",
		Title: "Gone quiet", Body: b.String(),
	})
}

func mostUrgent(open []comdomain.Commitment, now time.Time) *comdomain.Commitment {
	var best *comdomain.Commitment
	for i := range open {
		c := &open[i]
		if c.DueAt == nil {
			continue
		}
		// Anything due by the end of today counts.
		if c.DueAt.After(now.Add(24 * time.Hour)) {
			continue
		}
		if best == nil || c.DueAt.Before(*best.DueAt) {
			best = c
		}
	}
	return best
}

func (g *Generate) store(ctx context.Context, c domain.CheckIn) (domain.CheckIn, error) {
	created, err := g.repo.Create(ctx, c)
	if err != nil {
		return domain.CheckIn{}, err
	}
	log.Printf("check-in: %s (%s)", created.Title, created.Slot)
	g.notifier.Notify(created.Title, created.Body)
	return created, nil
}

// Open turns a check-in into a conversation the first time you reply to it, and
// returns the same one after that. Conversations are created lazily so that
// check-ins you never answer do not fill the chat list.
func (g *Generate) Open(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	c, err := g.repo.Get(ctx, id)
	if err != nil {
		return uuid.Nil, err
	}
	if c.SessionID != nil {
		return *c.SessionID, nil
	}

	session, err := g.chat.StartSession(ctx, c.Title, c.Body, c.ModeID)
	if err != nil {
		return uuid.Nil, err
	}
	sessionID, err := uuid.Parse(session.ID)
	if err != nil {
		return uuid.Nil, err
	}
	if err := g.repo.AttachSession(ctx, id, sessionID); err != nil {
		return uuid.Nil, err
	}
	return sessionID, nil
}

func (g *Generate) Dismiss(ctx context.Context, id uuid.UUID) error {
	return g.repo.MarkSeen(ctx, id)
}

func (g *Generate) Unseen(ctx context.Context) ([]domain.CheckIn, error) {
	return g.repo.Unseen(ctx, 5)
}
