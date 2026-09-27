package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
	"github.com/an4eetos/decision-room/internal/commitments/service"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

// Extract finds commitments you made in conversation and proposes them.
//
// Proposals, never facts. Everything extracted waits for you to keep or drop it,
// because a model deciding what you promised is exactly the kind of thing that
// is sometimes wrong, and an open-loop list you did not agree to stops being
// trusted and then stops being read.
type Extract struct {
	llm     memport.LLM
	repo    port.Repository
	enabled bool
	now     func() time.Time

	// busy caps extraction at one in flight. If a slow extraction is still
	// running when the next turn arrives, that turn is skipped rather than
	// queued: a missed proposal is cheap, a pile-up of goroutines holding model
	// calls is not.
	busy chan struct{}
}

func NewExtract(llm memport.LLM, repo port.Repository, enabled bool) *Extract {
	return &Extract{
		llm:     llm,
		repo:    repo,
		enabled: enabled,
		now:     time.Now,
		busy:    make(chan struct{}, 1),
	}
}

// extractTimeout bounds the background call. It is derived from a fresh context,
// not the request's — the request context is cancelled the moment the response
// is written, which would kill every extraction before it started.
const extractTimeout = 30 * time.Second

// ObserveTurn implements memport.TurnObserver. It returns immediately.
func (u *Extract) ObserveTurn(turn memport.Turn) {
	if !u.enabled || u.llm == nil {
		return
	}
	// The prefilter runs synchronously because it is free, and most turns stop
	// here without costing a model call.
	if !service.LooksLikeCommitment(turn.UserText) {
		return
	}

	select {
	case u.busy <- struct{}{}:
	default:
		log.Printf("commitments: extraction already running, skipping this turn")
		return
	}

	go func() {
		defer func() { <-u.busy }()

		ctx, cancel := context.WithTimeout(context.Background(), extractTimeout)
		defer cancel()

		if _, err := u.Run(ctx, turn); err != nil {
			log.Printf("commitments: extraction failed: %v", err)
		}
	}()
}

const extractPrompt = `You find commitments a person made in their own words.

A commitment is something THEY said they will do: "I'll ship it by Friday",
"I need to call the landlord". Only what the person wrote counts. Suggestions the
assistant made are not commitments unless the person explicitly agreed to them.
Wishes, questions and reflections are not commitments.

Today is %s (%s). Resolve relative dates like "Friday" or "tomorrow" against it.

Return ONLY a JSON array, no prose, no code fences:
[{"text": "<short imperative, max 12 words>", "due": "YYYY-MM-DD" or null, "confidence": 0.0-1.0}]

Return [] if they committed to nothing. That is the usual answer.`

// Run extracts synchronously and stores proposals. Exported for tests and for
// callers that want to wait on the result.
func (u *Extract) Run(ctx context.Context, turn memport.Turn) ([]domain.Commitment, error) {
	now := u.now()

	answer, err := u.llm.Chat(ctx, []memport.Message{
		{Role: "system", Content: fmt.Sprintf(extractPrompt, now.Format("2006-01-02"), now.Weekday())},
		{Role: "user", Content: "They wrote:\n" + turn.UserText +
			"\n\nThe assistant replied (context only — not their commitments):\n" +
			truncate(turn.AssistantText, 1500)},
	})
	if err != nil {
		return nil, fmt.Errorf("extraction call: %w", err)
	}

	candidates, err := service.ParseCandidates(answer, now)
	if err != nil {
		return nil, err
	}

	sessionID, messageID := turn.SessionID, turn.MessageID
	var created []domain.Commitment

	for _, c := range candidates {
		row, err := u.repo.Create(ctx, domain.Commitment{
			Text:        c.Text,
			Status:      domain.StatusProposed,
			DueAt:       c.Due,
			Source:      domain.SourceChat,
			SessionID:   &sessionID,
			MessageID:   &messageID,
			ModeID:      turn.ModeID,
			Confidence:  c.Confidence,
			Fingerprint: service.Fingerprint(c.Text),
		})
		// Already tracked is the expected outcome of saying the same thing twice.
		if errors.Is(err, port.ErrDuplicate) {
			continue
		}
		if err != nil {
			return created, err
		}
		created = append(created, row)
	}

	return created, nil
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
