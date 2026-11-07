package usecase

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	genservice "github.com/an4eetos/decision-room/internal/generals/service"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

func TestParseBeastFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		answer string
		clean  string
		orders []port.BeastOrder
		done   []int
	}{
		{
			name:   "no flags",
			answer: "Move.",
			clean:  "Move.",
		},
		{
			name:   "an order becomes a readable line",
			answer: "**Zhukov:** Move, сука.\n\n[[order: 20 | Send the draft to the landlord]]",
			clean:  "**Zhukov:** Move, сука.\n\n- ⏱ **20 min** — Send the draft to the landlord",
			orders: []port.BeastOrder{{Minutes: 20, Text: "Send the draft to the landlord"}},
		},
		{
			name:   "loop link, min suffix, bullet and stray spaces",
			answer: "Go.\n - [[order:25 min |  Pay the deposit | Loop 2 ]]  ",
			clean:  "Go.\n- ⏱ **25 min** — Pay the deposit",
			orders: []port.BeastOrder{{Minutes: 25, Text: "Pay the deposit", Loop: 2}},
		},
		{
			name:   "minutes are clamped",
			answer: "[[order: 0 | Stand up]]\n[[order: 9999 | Write the thesis]]",
			clean:  "- ⏱ **1 min** — Stand up\n- ⏱ **240 min** — Write the thesis",
			orders: []port.BeastOrder{{Minutes: 1, Text: "Stand up"}, {Minutes: 240, Text: "Write the thesis"}},
		},
		{
			name:   "more than three orders are cut",
			answer: "[[order: 5 | a]]\n[[order: 5 | b]]\n[[order: 5 | c]]\n[[order: 5 | d]]",
			clean:  "- ⏱ **5 min** — a\n- ⏱ **5 min** — b\n- ⏱ **5 min** — c",
			orders: []port.BeastOrder{{Minutes: 5, Text: "a"}, {Minutes: 5, Text: "b"}, {Minutes: 5, Text: "c"}},
		},
		{
			name:   "done lines are removed and de-duplicated",
			answer: "Good. Next.\n\n[[done: 1, 3]]\n[[done: 3]]\n\n[[order: 10 | Call the bank]]",
			clean:  "Good. Next.\n\n- ⏱ **10 min** — Call the bank",
			orders: []port.BeastOrder{{Minutes: 10, Text: "Call the bank"}},
			done:   []int{1, 3},
		},
		{
			name:   "a mention inside prose is left alone",
			answer: "Do not write [[order: 5 | x]] in the middle of a sentence.",
			clean:  "Do not write [[order: 5 | x]] in the middle of a sentence.",
		},
		{
			name:   "an order with no minutes is not an order",
			answer: "[[order: soon | whenever]]",
			clean:  "[[order: soon | whenever]]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clean, orders, done := parseBeastFlags(tc.answer)
			if clean != tc.clean {
				t.Errorf("clean =\n%q\nwant\n%q", clean, tc.clean)
			}
			if !slices.Equal(orders, tc.orders) {
				t.Errorf("orders = %+v, want %+v", orders, tc.orders)
			}
			if !slices.Equal(done, tc.done) {
				t.Errorf("done = %v, want %v", done, tc.done)
			}
		})
	}
}

func TestShouterIsTheMostAggressiveSeated(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	ranking := testMode(t, "beast").Shouters

	cases := []struct {
		seated []string
		want   string
	}{
		{[]string{"kutuzov", "patton", "zhukov"}, "zhukov"},
		{[]string{"slim", "chuikov", "patton"}, "patton"},
		// Nobody on the ranking: whoever sits first takes the voice.
		{[]string{"kutuzov", "slim"}, "kutuzov"},
	}
	for _, tc := range cases {
		got, ok := pickShouter(reg.Resolve(tc.seated), ranking)
		if !ok || got.ID != tc.want {
			t.Errorf("seated %v: shouter %q, want %q", tc.seated, got.ID, tc.want)
		}
	}
	if _, ok := pickShouter(nil, ranking); ok {
		t.Error("no one seated means no shouter")
	}
}

func beastPlan(t *testing.T, conclude bool) ConsultPlan {
	t.Helper()
	reg := testRegistry(t)
	now := time.Date(2026, 10, 11, 18, 0, 0, 0, time.UTC)
	due := now.Add(5 * time.Hour)
	return ConsultPlan{
		Mode:     testMode(t, "beast"),
		Generals: reg.Resolve([]string{"kutuzov", "zhukov"}),
		Traps:    reg.Traps(),
		Tier:     domain.PolicyFor(domain.TierStandard),
		Conclude: conclude,
		Beast: &port.BeastBrief{
			Now: now,
			Tasks: []port.BeastTask{
				{Number: 1, Text: "Open the report", Minutes: 10, DeadlineAt: now.Add(-4 * time.Minute), Status: "open", Expiries: 2},
				{Number: 2, Text: "Email Aigerim", Minutes: 20, DeadlineAt: now.Add(12 * time.Minute), Status: "open"},
				{Number: 3, Text: "Book the flat", Status: "abandoned"},
			},
			Loops: []port.BeastLoop{{Number: 1, Text: "Submit the visa form", DueAt: &due}},
			Event: port.BeastEvent{Kind: "expired", Tasks: []port.BeastTask{{Number: 1, Text: "Open the report"}}},
		},
	}
}

func TestBeastPromptPutsTheShouterOnTheClock(t *testing.T) {
	t.Parallel()

	prompt := buildSystemPrompt(systemPrompt, beastPlan(t, false))

	for _, want := range []string{
		"This is a beast mode",
		"Zhukov is the shouter",
		"they do not swear",
		"сука", "penal battalion",
		"No slurs about race",
		"drop the act completely",
		`Task 1: "Open the report", OVERDUE by 4 minutes, deadline missed 2×`,
		`Task 2: "Email Aigerim", 12 minutes left`,
		`Task 3: "Book the flat", abandoned (cowardice)`,
		`Loop 1: "Submit the visa form", due today`,
		"the clock ran out on Task 1",
		"[[order: MINUTES | the physical action]]",
		"never put it on a step toward it",
		"[[done: N]]",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("beast prompt missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"not as characters to perform", "## The fork", "[[interrogate:", "Rules for the interrogation",
	} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("beast prompt should not contain %q", unwanted)
		}
	}
}

func TestStandDownDebriefsWithoutNewClocks(t *testing.T) {
	t.Parallel()

	prompt := buildSystemPrompt(systemPrompt, beastPlan(t, true))

	for _, want := range []string{"**Stand down**", "**Taken**", "**Abandoned**", "**Next contact**", "No order lines"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("stand-down prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "[[order:") {
		t.Error("standing down must not hand out new orders")
	}
}

func TestNoInterrogationIsOfferedMidCrisis(t *testing.T) {
	t.Parallel()

	plan := beastPlan(t, false)
	plan.TrapHits = genservice.DetectTraps("I have no choice, I'm completely stuck and everyone will laugh at me", plan.Traps)
	if len(plan.TrapHits) == 0 {
		t.Fatal("the test question should trip a trap")
	}

	// The same words outside beast mode do earn a suggestion, so the nil
	// below is the mode's doing.
	ordinary := plan
	ordinary.Mode = testMode(t, "hard_call")
	if suggest(ordinary, nil) == nil {
		t.Fatal("outside beast mode these words should suggest an interrogation")
	}
	if s := suggest(plan, nil); s != nil {
		t.Fatalf("beast mode must not suggest an interrogation, got %+v", s)
	}
}

// beastChatRepo is a session already locked into beast mode.
type beastChatRepo struct {
	stubChatRepo
	session  port.ChatSession
	messages []port.ChatMessage
	created  []port.ChatMessage
}

func (r *beastChatRepo) GetSession(ctx context.Context, id uuid.UUID) (port.ChatSession, error) {
	return r.session, nil
}

func (r *beastChatRepo) ListMessages(ctx context.Context, id uuid.UUID) ([]port.ChatMessage, error) {
	return r.messages, nil
}

func (r *beastChatRepo) CreateMessage(ctx context.Context, m port.ChatMessage) (port.ChatMessage, error) {
	r.created = append(r.created, m)
	return m, nil
}

type stubBoard struct {
	expired  []port.BeastTask
	expires  int
	reported []uuid.UUID
}

func (b *stubBoard) Tasks(context.Context, uuid.UUID) ([]port.BeastTask, error) { return nil, nil }
func (b *stubBoard) DueLoops(context.Context, time.Time) ([]port.BeastLoop, error) {
	return nil, nil
}
func (b *stubBoard) Expire(context.Context, uuid.UUID, time.Time) ([]port.BeastTask, error) {
	b.expires++
	return b.expired, nil
}
func (b *stubBoard) Report(_ context.Context, _ uuid.UUID, ids []uuid.UUID) ([]port.BeastTask, error) {
	b.reported = append(b.reported, ids...)
	return nil, nil
}
func (b *stubBoard) Apply(context.Context, uuid.UUID, uuid.UUID, []port.BeastOrder, []int, []port.BeastLoop) error {
	return nil
}
func (b *stubBoard) StandDown(context.Context, uuid.UUID) error { return nil }

// An expiry turn that finds nothing expired leaves no message behind and
// never reaches the model.
func TestExpiryWithNothingExpiredCostsNothing(t *testing.T) {
	t.Parallel()

	repo := &beastChatRepo{session: port.ChatSession{ID: uuid.New(), ModeID: beastModeID, ModeLocked: true}}
	board := &stubBoard{}
	chat := NewChat(repo, nil, nil).WithBeastBoard(board)

	_, err := chat.SendMessage(context.Background(), SendMessageInput{
		SessionID: repo.session.ID.String(),
		Question:  "⏱ Time's up",
		Beast:     &BeastEventInput{Kind: "expired"},
	})
	if !errors.Is(err, port.ErrNothingExpired) {
		t.Fatalf("got %v, want ErrNothingExpired", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("stored %d messages for an expiry that did not happen", len(repo.created))
	}
	if board.expires != 1 {
		t.Fatalf("the board should have been asked once, was asked %d times", board.expires)
	}
}

func TestEruptionsAreSpacedOut(t *testing.T) {
	t.Parallel()

	repo := &beastChatRepo{
		session: port.ChatSession{ID: uuid.New(), ModeID: beastModeID, ModeLocked: true},
		messages: []port.ChatMessage{
			{Role: "user", Content: expiryPrefix + " task 1, Open the report. Not reported.", CreatedAt: time.Now().Add(-30 * time.Second)},
			{Role: "assistant", Content: "Move!", ModeID: beastModeID},
		},
	}
	board := &stubBoard{expired: []port.BeastTask{{Number: 2, Text: "Email"}}}
	chat := NewChat(repo, nil, nil).WithBeastBoard(board)

	_, err := chat.SendMessage(context.Background(), SendMessageInput{
		SessionID: repo.session.ID.String(),
		Beast:     &BeastEventInput{Kind: "expired"},
	})
	if !errors.Is(err, port.ErrEruptionTooSoon) {
		t.Fatalf("got %v, want ErrEruptionTooSoon", err)
	}
	if board.expires != 0 {
		t.Fatal("a refused eruption must not re-arm anything; the next one shouts about it")
	}
}

func TestExpiryQuestionNamesTheTasks(t *testing.T) {
	t.Parallel()

	got := expiryQuestion([]port.BeastTask{{Number: 1, Text: "Open the report"}, {Number: 3, Text: "Call"}})
	want := "⏱ Time's up: task 1, Open the report; task 3, Call. Not reported."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
