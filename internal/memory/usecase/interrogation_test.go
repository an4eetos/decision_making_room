package usecase

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	modefs "github.com/an4eetos/decision-room/internal/modes/adapters/driven/fs"
	modeassets "github.com/an4eetos/decision-room/internal/modes/assets"
	modedomain "github.com/an4eetos/decision-room/internal/modes/domain"
)

func testMode(t *testing.T, id string) modedomain.Mode {
	t.Helper()
	reg, err := modefs.Load(modeassets.Modes(), "")
	if err != nil {
		t.Fatalf("load modes: %v", err)
	}
	mode, ok := modefs.NewRegistry(reg).Get(id)
	if !ok {
		t.Fatalf("no %q mode", id)
	}
	return mode
}

func TestParseInterrogateFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		answer    string
		wantClean string
		wantIDs   []string
	}{
		{"absent", "Just an answer.", "Just an answer.", nil},
		{"one line at the end", "Answer.\n\n[[interrogate: spotlight, sunk_cost]]", "Answer.", []string{"spotlight", "sunk_cost"}},
		{"spaces and case", "Answer.\n  [[interrogate:  Spotlight ,SUNK_COST ]]  \n", "Answer.", []string{"spotlight", "sunk_cost"}},
		{"written twice", "A\n[[interrogate: spotlight]]\nB\n[[interrogate: spotlight, encircled]]", "A\n\nB", []string{"spotlight", "encircled"}},
		{"empty list", "Answer.\n[[interrogate: ]]", "Answer.", nil},
		// Seen from Gemini: the prefix dropped, only the ids left.
		{"bare ids", "Answer.\n\n[[catastrophizing, loss_aversion, spotlight]]", "Answer.", []string{"catastrophizing", "loss_aversion", "spotlight"}},
		{"a bracketed phrase is prose", "Answer.\n[[See the March note.]]", "Answer.\n[[See the March note.]]", nil},
		// Mid-sentence it is prose, not a flag; leave it alone.
		{"inline is not a flag", "Write [[interrogate: x]] at the end.", "Write [[interrogate: x]] at the end.", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clean, ids := parseInterrogateFlag(tc.answer)
			if clean != tc.wantClean {
				t.Fatalf("clean = %q, want %q", clean, tc.wantClean)
			}
			if !slices.Equal(ids, tc.wantIDs) {
				t.Fatalf("ids = %v, want %v", ids, tc.wantIDs)
			}
		})
	}
}

func suggestPlan(t *testing.T, question string, seated ...string) ConsultPlan {
	t.Helper()
	return NewPlanResolver(testRegistry(t), testDetector(t), domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{Question: question, GeneralIDs: seated})
}

func TestSuggestFromPhrasesIsSignedByTheGeneralWhoKillsTheTrap(t *testing.T) {
	t.Parallel()

	// Patton kills the spotlight effect; Kutuzov does too, but Patton is first
	// and Zhukov kills neither.
	plan := suggestPlan(t, "Everyone at work will think I'm weak if I take the leave.", "zhukov", "patton")
	s := suggest(plan, nil)
	if s == nil {
		t.Fatal("expected a suggestion")
	}
	if s.By != "patton" {
		t.Fatalf("signed by %q, want patton", s.By)
	}
	if s.Source != domain.SuggestionSignal || s.Traps[0].ID != "spotlight" || s.Traps[0].Quote == "" {
		t.Fatalf("unexpected suggestion: %+v", s)
	}
}

func TestSuggestMergesModelFlags(t *testing.T) {
	t.Parallel()

	plan := suggestPlan(t, "I already put three years into this company.", "rommel")

	s := suggest(plan, []string{"sunk_cost", "spotlight", "made_up"})
	if s == nil {
		t.Fatal("expected a suggestion")
	}
	if s.Source != domain.SuggestionBoth {
		t.Fatalf("source = %q, want both", s.Source)
	}
	ids := make([]string, 0, len(s.Traps))
	for _, f := range s.Traps {
		ids = append(ids, f.ID)
	}
	if !slices.Equal(ids, []string{"sunk_cost", "spotlight"}) {
		t.Fatalf("traps = %v; unknown ids must be dropped", ids)
	}
	if s.Traps[0].Source != domain.SuggestionBoth || s.Traps[1].Source != domain.SuggestionModel {
		t.Fatalf("per-trap sources wrong: %+v", s.Traps)
	}
}

func TestSuggestNeedsAStrongTrap(t *testing.T) {
	t.Parallel()

	plan := suggestPlan(t, "Plan my week.")
	if s := suggest(plan, nil); s != nil {
		t.Fatalf("nothing found, got %+v", s)
	}
	// Only weak traps flagged: ordinary speech, not a reason to interrupt.
	if s := suggest(plan, []string{"vague_intent", "planning_fallacy"}); s != nil {
		t.Fatalf("weak-only flags should not suggest, got %+v", s)
	}
	// A strong one flagged by the model alone is enough.
	if s := suggest(plan, []string{"vague_intent", "encircled"}); s == nil || s.Traps[0].ID != "encircled" {
		t.Fatalf("strong trap should lead, got %+v", s)
	}
}

func TestNoSuggestionOnPlainOrInsideAnInterrogation(t *testing.T) {
	t.Parallel()

	resolver := NewPlanResolver(testRegistry(t), testDetector(t), domain.TierStandard, domain.TierDeep)
	question := "I have no choice, everyone will think I'm a failure."

	plain := resolver.Resolve(ConsultInput{Question: question, Plain: true})
	if s := suggest(plain, []string{"encircled"}); s != nil {
		t.Fatal("a check-in must never suggest an interrogation")
	}

	inside := resolver.Resolve(ConsultInput{Question: question, ModeID: "interrogation"})
	if len(inside.TrapHits) == 0 {
		t.Fatal("inside an interrogation the hits feed the interrogators")
	}
	if s := suggest(inside, []string{"encircled"}); s != nil {
		t.Fatal("an interrogation must not suggest an interrogation")
	}
}

// Entering an interrogation keeps the generals who saw the trap, where any other
// mode switch would pick fresh ones.
func TestInterrogationKeepsTheSeatedGenerals(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(testRegistry(t), testDetector(t), domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{
			Question:       "Interrogate me on this.",
			ModeID:         "interrogation",
			SessionMode:    "open",
			SeatedGenerals: []string{"kutuzov", "patton"},
			TurnIndex:      2,
		})

	if !plan.Mode.IsInterrogation() {
		t.Fatalf("mode = %q", plan.Mode.ID)
	}
	if got := plan.GeneralIDs(); len(got) == 0 || got[0] != "kutuzov" || plan.GeneralsMethod != "sticky" {
		t.Fatalf("seated roster not kept: %v (%s)", got, plan.GeneralsMethod)
	}
}

func TestInterrogationPromptQuestionsInsteadOfDebating(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	plan := ConsultPlan{
		Mode:     testMode(t, "interrogation"),
		Generals: reg.Resolve([]string{"zhukov", "kutuzov"}),
		Traps:    reg.Traps(),
		Tier:     domain.PolicyFor(domain.TierStandard),
	}
	prompt := buildSystemPrompt(systemPrompt, plan)

	for _, want := range []string{
		"This is an interrogation", "**On the record**", "**Questions**",
		"Zhukov's orders:", "Kutuzov hunts:", "Encirclement passivity", "Vague intent",
		"Spotlight effect", "**Name:** question",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("interrogation prompt missing %q", want)
		}
	}
	for _, unwanted := range []string{"## The fork", "## The call", "[[interrogate:"} {
		if strings.Contains(prompt, unwanted) {
			t.Errorf("interrogation prompt should not contain %q", unwanted)
		}
	}
	// Traps none of the seated lenses kill stay out.
	if strings.Contains(prompt, "Planning fallacy") {
		t.Error("only the seated lenses' traps belong in the prompt")
	}
}

func TestConcludeSwitchesToThePosition(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	plan := ConsultPlan{
		Mode:     testMode(t, "interrogation"),
		Generals: reg.Resolve([]string{"zhukov"}),
		Traps:    reg.Traps(),
		Tier:     domain.PolicyFor(domain.TierStandard),
		Conclude: true,
	}
	prompt := buildSystemPrompt(systemPrompt, plan)

	for _, want := range []string{"**Position**", "**Killed**", "**Orders**", "**Accepted dark**", "Do not ask anything more"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("closing prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "**Still dark**") {
		t.Error("the closing turn must not keep the questioning template")
	}
}

func TestOrdinaryAnswersCarryTheFlagAndPlainOnesDoNot(t *testing.T) {
	t.Parallel()

	reg := testRegistry(t)
	plan := ConsultPlan{
		Mode:     testMode(t, "hard_call"),
		Generals: reg.Resolve([]string{"zhukov"}),
		Traps:    reg.Traps(),
		Tier:     domain.PolicyFor(domain.TierStandard),
	}
	if !strings.Contains(buildSystemPrompt(systemPrompt, plan), "[[interrogate: id, id]]") {
		t.Fatal("an ordinary answer should be able to flag a trap")
	}

	plan.Plain = true
	if strings.Contains(buildSystemPrompt(systemPrompt, plan), "[[interrogate") {
		t.Fatal("a plain check-in must not carry the flag instruction")
	}
}

func TestSuggestionCooldown(t *testing.T) {
	t.Parallel()

	answer := func(s *domain.Suggestion, mode string) port.ChatMessage {
		return port.ChatMessage{Role: "assistant", Suggestion: s, ModeID: mode}
	}
	user := port.ChatMessage{Role: "user"}
	made := &domain.Suggestion{Traps: []domain.TrapFound{{ID: "spotlight"}}}

	if suggestedRecently([]port.ChatMessage{user}) {
		t.Fatal("a fresh conversation has made no suggestion")
	}
	recent := []port.ChatMessage{user, answer(made, "open"), user, answer(nil, "open"), user}
	if !suggestedRecently(recent) {
		t.Fatal("a suggestion two answers ago should hold the next one back")
	}
	old := []port.ChatMessage{answer(made, "open")}
	for range suggestionCooldown {
		old = append(old, user, answer(nil, "open"))
	}
	if suggestedRecently(append(old, user)) {
		t.Fatal("the cooldown should expire")
	}
	if !suggestedRecently([]port.ChatMessage{answer(nil, interrogationModeID), user}) {
		t.Fatal("just after an interrogation there is nothing to suggest")
	}
}

// modeRepo records the last session update, for SetMode.
type modeRepo struct {
	stubChatRepo
	updated port.ChatSession
}

func (m *modeRepo) GetSession(_ context.Context, id uuid.UUID) (port.ChatSession, error) {
	return port.ChatSession{ID: id, ModeID: "interrogation", ModeLocked: true}, nil
}

func (m *modeRepo) UpdateSession(_ context.Context, session port.ChatSession) error {
	m.updated = session
	return nil
}

// Leaving an interrogation has to unlock the session at once, or reopening the
// conversation would put you straight back in it.
func TestSetModeUnlocksOnLeave(t *testing.T) {
	t.Parallel()

	repo := &modeRepo{}
	chat := NewChat(repo, nil, nil)
	if _, err := chat.SetMode(context.Background(), uuid.NewString(), ""); err != nil {
		t.Fatal(err)
	}
	if repo.updated.ModeLocked || repo.updated.ModeID != "" {
		t.Fatalf("leaving should unlock and clear the mode, got %+v", repo.updated)
	}

	if _, err := chat.SetMode(context.Background(), uuid.NewString(), "interrogation"); err != nil {
		t.Fatal(err)
	}
	if !repo.updated.ModeLocked || repo.updated.ModeID != "interrogation" {
		t.Fatalf("entering should lock the mode, got %+v", repo.updated)
	}

	if _, err := chat.SetMode(context.Background(), "not-a-uuid", ""); err == nil {
		t.Fatal("an invalid id should fail")
	}
}

// A trap the person's words showed is hunted whoever is seated, so it is not
// misnamed after the nearest trap a seated lens carries.
func TestInterrogationHuntsTrapsFromTheConversation(t *testing.T) {
	t.Parallel()

	plan := NewPlanResolver(testRegistry(t), testDetector(t), domain.TierStandard, domain.TierDeep).
		Resolve(ConsultInput{
			Question:       "Interrogate me on this.",
			ModeID:         "interrogation",
			GeneralIDs:     []string{"kutuzov"},
			History:        []port.Message{{Role: "user", Content: "I already put three years into this company."}},
			SeatedGenerals: []string{"kutuzov"},
		})
	if !slices.Contains(plan.SpottedTraps(), "sunk_cost") {
		t.Fatalf("sunk cost in the history should be spotted, got %v", plan.SpottedTraps())
	}
	if prompt := buildSystemPrompt(systemPrompt, plan); !strings.Contains(prompt, "Sunk cost (sunk_cost)") {
		t.Fatal("a spotted trap must reach the interrogation prompt even when no seated lens kills it")
	}
}
