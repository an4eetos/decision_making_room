package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/service"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comport "github.com/an4eetos/decision-room/internal/commitments/port"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

// interrogationModeID is the shipped interrogation mode, whose answers already
// name unknowns and orders in a fixed shape.
const interrogationModeID = "interrogation"

// Extract maps what you write onto the campaign as proposals.
//
// Two paths. An ordinary turn costs one background model call, and only when
// the free prefilter sees goal, blocker or uncertainty language in what you
// wrote. An interrogation turn costs nothing: its answer already lists what is
// still dark and the probe to send, so they are read straight out of it.
type Extract struct {
	llm     memport.LLM
	repo    port.Repository
	orders  *comusecase.Manage
	enabled bool
	now     func() time.Time
	// busy caps model extraction at one in flight; a turn arriving while one
	// runs is skipped rather than queued, the same rule as open loops.
	busy chan struct{}
}

func NewExtract(llm memport.LLM, repo port.Repository, orders *comusecase.Manage, enabled bool) *Extract {
	return &Extract{
		llm:     llm,
		repo:    repo,
		orders:  orders,
		enabled: enabled,
		now:     time.Now,
		busy:    make(chan struct{}, 1),
	}
}

// extractTimeout bounds the background work, on a fresh context: the request's
// is cancelled the moment the response is written.
const extractTimeout = 30 * time.Second

// interrogationConfidence is what intel read out of an interrogation carries.
// The shape is certain; whether each unknown matters to you is not, which is
// why it is still only a proposal.
const interrogationConfidence = 0.8

// maxProposedOrders bounds the orders one closing turn proposes.
const maxProposedOrders = 3

// ObserveTurn implements memport.TurnObserver. It returns immediately.
func (u *Extract) ObserveTurn(turn memport.Turn) {
	if !u.enabled {
		return
	}

	if turn.ModeID == interrogationModeID {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
			defer cancel()
			if err := u.FromInterrogation(ctx, turn); err != nil {
				log.Printf("campaign: reading interrogation: %v", err)
			}
		}()
		return
	}

	if u.llm == nil || !service.LooksLikeIntel(turn.UserText) {
		return
	}
	select {
	case u.busy <- struct{}{}:
	default:
		log.Printf("campaign: extraction already running, skipping this turn")
		return
	}
	go func() {
		defer func() { <-u.busy }()
		ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
		defer cancel()
		if _, err := u.Run(ctx, turn); err != nil {
			log.Printf("campaign: extraction failed: %v", err)
		}
	}()
}

const extractPrompt = `You map a person's campaign from what they wrote: the goals they are pursuing, what stands in their way, and what they do not know yet.

Only what THEY wrote counts. The assistant's reply is context, never a source: a goal or a risk the assistant raised is not theirs unless they said it.

- objectives: an outcome they are working toward, as a position to take ("Move to Lisbon by March", "Ship the beta"). Not today's tasks — those are tracked elsewhere.
- obstacles: something concrete between them and an objective — a blocker, a missing input, a fear. Give its kind of stuck:
  undefined (the next step is not defined), waiting (waiting on input they have not chased), fear (afraid of what the result will show), too_big (too large to hold in one sitting), unwanted (they do not actually want the outcome).
  And an estimated strength: 1 outpost (one move clears it), 2 dug in (takes sustained effort), 3 fortress (may not fall; going around may be better).
- unknowns: a question whose answer would change what they do, written as a question.

%s

Today is %s (%s). Resolve relative dates against it.

Return ONLY a JSON object, no prose, no code fences:
{"objectives": [{"text": "...", "front": <front name or null>, "due": "YYYY-MM-DD" or null, "confidence": 0.0-1.0}],
 "obstacles": [{"text": "...", "front": <front name or null>, "objective": <objective number, or the text of an objective in this answer, or null>, "kind": "...", "strength": 1-3, "confidence": 0.0-1.0}],
 "unknowns": [{"text": "...?", "front": <front name or null>, "objective": <as above>, "confidence": 0.0-1.0}]}

Do not repeat anything already on the map. Empty arrays are the usual answer.`

// Run extracts synchronously and stores proposals. Exported for tests.
func (u *Extract) Run(ctx context.Context, turn memport.Turn) ([]domain.Item, error) {
	now := u.now()

	fronts, err := u.repo.ListFronts(ctx, true)
	if err != nil {
		return nil, err
	}
	live, err := u.repo.ListItems(ctx, []domain.Status{domain.StatusProposed, domain.StatusActive}, 300)
	if err != nil {
		return nil, err
	}
	var objectives []domain.Item
	for _, it := range live {
		if it.Type == domain.TypeObjective && it.Status == domain.StatusActive {
			objectives = append(objectives, it)
		}
	}

	answer, err := u.llm.Chat(ctx, []memport.Message{
		{Role: "system", Content: fmt.Sprintf(extractPrompt, mapContext(fronts, objectives, live), now.Format("2006-01-02"), now.Weekday())},
		{Role: "user", Content: "They wrote:\n" + turn.UserText +
			"\n\nThe assistant replied (context only — not their goals):\n" + truncate(turn.AssistantText, 1500)},
	})
	if err != nil {
		return nil, fmt.Errorf("extraction call: %w", err)
	}

	candidates, err := service.ParseCandidates(answer, now)
	if err != nil {
		return nil, err
	}

	byName := make(map[string]uuid.UUID, len(fronts))
	for _, f := range fronts {
		byName[strings.ToLower(f.Name)] = f.ID
	}

	sessionID, messageID := turn.SessionID, turn.MessageID
	// New objectives first, so an obstacle in the same answer can point at one.
	newObjectives := map[string]uuid.UUID{}
	var created []domain.Item

	for _, c := range candidates {
		item := domain.Item{
			Type:        c.Type,
			Text:        c.Text,
			Status:      domain.StatusProposed,
			Kind:        c.Kind,
			Strength:    c.Strength,
			DueAt:       c.Due,
			Source:      domain.SourceChat,
			SessionID:   &sessionID,
			MessageID:   &messageID,
			Confidence:  c.Confidence,
			Fingerprint: service.Fingerprint(c.Text),
		}
		// Extraction never invents a front: an unknown name is no front.
		if id, ok := byName[strings.ToLower(c.Front)]; ok {
			item.FrontID = &id
		}
		switch {
		case c.ObjectiveIndex > 0 && c.ObjectiveIndex <= len(objectives):
			id := objectives[c.ObjectiveIndex-1].ID
			item.ObjectiveID = &id
		case c.ObjectiveText != "":
			if id, ok := newObjectives[service.Fingerprint(c.ObjectiveText)]; ok {
				item.ObjectiveID = &id
			}
		}
		// Under an objective with a front, the item is on that front too.
		if item.FrontID == nil && item.ObjectiveID != nil {
			for _, o := range objectives {
				if o.ID == *item.ObjectiveID {
					item.FrontID = o.FrontID
				}
			}
		}

		row, err := u.repo.CreateItem(ctx, item)
		if errors.Is(err, port.ErrDuplicate) {
			continue
		}
		if err != nil {
			return created, err
		}
		if row.Type == domain.TypeObjective {
			newObjectives[row.Fingerprint] = row.ID
		}
		created = append(created, row)
	}
	return created, nil
}

// mapContext tells the model what is already there: the fronts it may file
// under, the objectives it may link to by number, and what not to repeat.
func mapContext(fronts []domain.Front, objectives, live []domain.Item) string {
	var b strings.Builder
	if len(fronts) == 0 {
		b.WriteString("They have no fronts yet. Set every \"front\" to null.\n")
	} else {
		names := make([]string, 0, len(fronts))
		for _, f := range fronts {
			names = append(names, f.Name)
		}
		fmt.Fprintf(&b, "Their fronts (areas of life and work): %s. Set \"front\" to one of these exact names, or null. Never invent a front.\n",
			strings.Join(names, ", "))
	}
	if len(objectives) > 0 {
		b.WriteString("Their current objectives, numbered:\n")
		for i, o := range objectives {
			fmt.Fprintf(&b, "%d. %s\n", i+1, o.Text)
		}
	}
	var already []string
	for _, it := range live {
		if it.Type != domain.TypeObjective || it.Status == domain.StatusProposed {
			already = append(already, fmt.Sprintf("- %s: %s", it.Type, it.Text))
		}
		if len(already) == 20 {
			break
		}
	}
	if len(already) > 0 {
		b.WriteString("Also already on the map:\n")
		b.WriteString(strings.Join(already, "\n"))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// FromInterrogation reads an interrogation answer into proposals with no model
// call: what is still dark becomes fog, the probe becomes a recon order aimed
// at the unknown it plainly scouts, and a closing turn's orders become orders.
func (u *Extract) FromInterrogation(ctx context.Context, turn memport.Turn) error {
	intel := service.ParseInterrogation(turn.AssistantText)
	sessionID, messageID := turn.SessionID, turn.MessageID

	for _, text := range intel.Unknowns {
		if len([]rune(text)) > 200 {
			continue
		}
		_, err := u.repo.CreateItem(ctx, domain.Item{
			Type:        domain.TypeUnknown,
			Text:        text,
			Status:      domain.StatusProposed,
			Source:      domain.SourceInterrogation,
			SessionID:   &sessionID,
			MessageID:   &messageID,
			Confidence:  interrogationConfidence,
			Fingerprint: service.Fingerprint(text),
		})
		if errors.Is(err, port.ErrDuplicate) {
			continue
		}
		if err != nil {
			return err
		}
	}

	if u.orders == nil {
		return nil
	}

	if intel.Probe != "" {
		if err := u.proposeProbe(ctx, intel.Probe, sessionID, messageID); err != nil {
			return err
		}
	}

	for i, order := range intel.Orders {
		if i == maxProposedOrders {
			break
		}
		if _, err := u.orders.Propose(ctx, comusecase.CreateInput{
			Text: order, SessionID: &sessionID, MessageID: &messageID, ModeID: interrogationModeID,
		}, interrogationConfidence); err != nil && !isDuplicateOrder(err) {
			log.Printf("campaign: proposing an order from the position: %v", err)
		}
	}
	return nil
}

// proposeProbe proposes the probe as an order aimed at what it plainly goes
// after: an unknown, which makes it reconnaissance, or an obstacle it attacks.
// Aimed by the words they share, with no model call; a probe that matches
// nothing is still worth proposing, just unaimed.
func (u *Extract) proposeProbe(ctx context.Context, probe string, sessionID, messageID uuid.UUID) error {
	live, err := u.repo.ListItems(ctx, []domain.Status{domain.StatusProposed, domain.StatusActive}, 300)
	if err != nil {
		return err
	}

	// One shared meaningful word is enough: the order is only a proposal, and a
	// wrong aim is one select away from fixed. Fog wins a tie, because scouting
	// is what a probe is for.
	var target *domain.Item
	best := 0
	for _, it := range live {
		if it.Type == domain.TypeObjective {
			continue
		}
		n := service.Overlap(probe, it.Text)
		if n > best || (n == best && n > 0 && it.Type == domain.TypeUnknown && target.Type != domain.TypeUnknown) {
			item := it
			target, best = &item, n
		}
	}

	in := comusecase.CreateInput{Text: probe, SessionID: &sessionID, MessageID: &messageID, ModeID: interrogationModeID}
	if target != nil {
		id := target.ID
		in.TargetID = &id
		if target.Type == domain.TypeUnknown {
			in.Kind = comdomain.KindRecon
		}
	}
	if _, err := u.orders.Propose(ctx, in, interrogationConfidence); err != nil && !isDuplicateOrder(err) {
		return err
	}
	return nil
}

func isDuplicateOrder(err error) bool {
	return errors.Is(err, comport.ErrDuplicate)
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
