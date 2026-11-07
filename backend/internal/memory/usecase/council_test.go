package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	modedomain "github.com/an4eetos/decision-room/internal/modes/domain"
)

// councilLLM answers a seat with its lens's position and the closing call with
// a fixed exchange, recording what each was sent. Seats run concurrently.
type councilLLM struct {
	mu        sync.Mutex
	seats     map[string]string // lens name -> its system prompt
	synthesis []port.Message
	fail      map[string]bool
}

const seatMarker = "You hold one seat on a council, as the "

func (f *councilLLM) respond(messages []port.Message) (string, error) {
	system := messages[0].Content
	f.mu.Lock()
	defer f.mu.Unlock()

	if i := strings.Index(system, seatMarker); i >= 0 {
		name := system[i+len(seatMarker):]
		name = name[:strings.Index(name, " lens")]
		if f.seats == nil {
			f.seats = map[string]string{}
		}
		f.seats[name] = system
		if f.fail[name] {
			return "", errors.New("overloaded")
		}
		return "**Position:** " + name + " says move now.", nil
	}

	f.synthesis = messages
	return "## The call\nMove.", nil
}

func (f *councilLLM) Chat(_ context.Context, messages []port.Message) (string, error) {
	return f.respond(messages)
}

func (f *councilLLM) ChatTools(_ context.Context, messages []port.Message, _ []port.Tool) (port.ChatTurn, error) {
	text, err := f.respond(messages)
	return port.ChatTurn{Content: text}, err
}

func councilPlan() ConsultPlan {
	zhukov := gendomain.Lens{ID: "zhukov", Name: "Zhukov", Job: "concentrate force"}
	kutuzov := gendomain.Lens{ID: "kutuzov", Name: "Kutuzov", Job: "trade space for time"}
	return ConsultPlan{
		Question: "Should I move to Lisbon in March?",
		Tier:     domain.PolicyFor(domain.TierDeep),
		Generals: []gendomain.Lens{zhukov, kutuzov},
		Doctrine: map[string][]gendomain.Passage{
			"zhukov":  {{LensID: "zhukov", Section: "Mass", Text: "ZHUKOV-PASSAGE"}},
			"kutuzov": {{LensID: "kutuzov", Section: "Retreat", Text: "KUTUZOV-PASSAGE"}},
		},
		Now: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC),
	}
}

func TestCouncilSeatsArgueAloneThenOneCallWritesTheExchange(t *testing.T) {
	t.Parallel()

	llm := &councilLLM{}
	council := NewCouncil(llm, nil, stubInitialContext{content: "About me."})

	var mu sync.Mutex
	var stages []string
	var streamed strings.Builder
	plan := councilPlan()
	plan.Progress = &Progress{
		OnStage: func(stage, detail string) {
			mu.Lock()
			defer mu.Unlock()
			stages = append(stages, stage+": "+detail)
		},
		OnDelta: func(text string) { streamed.WriteString(text) },
	}

	result, err := council.Execute(context.Background(), plan, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Answer != "## The call\nMove." {
		t.Fatalf("answer = %q", result.Answer)
	}

	zhukov := llm.seats["Zhukov"]
	if !strings.Contains(zhukov, "ZHUKOV-PASSAGE") || strings.Contains(zhukov, "KUTUZOV-PASSAGE") {
		t.Fatalf("a seat must carry its own doctrine and no other's:\n%s", zhukov)
	}
	if !strings.Contains(zhukov, "Kutuzov (trade space for time)") {
		t.Fatalf("a seat should know who else sits:\n%s", zhukov)
	}

	system := llm.synthesis[0].Content
	if !strings.Contains(system, "Their positions are in the last message") || !strings.Contains(system, "## Zhukov —") {
		t.Fatalf("closing call is missing the council's exchange shape:\n%s", system)
	}
	last := llm.synthesis[len(llm.synthesis)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "Zhukov says move now") || !strings.Contains(last.Content, "Kutuzov says move now") {
		t.Fatalf("positions should arrive as the last user turn, got %#v", last)
	}

	if !strings.HasPrefix(stages[0], StageCouncil+": Zhukov and Kutuzov are each working") {
		t.Fatalf("stages = %q", stages)
	}
}

func TestCouncilWritesTheExchangeBetweenThoseWhoReported(t *testing.T) {
	t.Parallel()

	llm := &councilLLM{fail: map[string]bool{"Kutuzov": true}}
	council := NewCouncil(llm, nil, stubInitialContext{})

	result, err := council.Execute(context.Background(), councilPlan(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	last := llm.synthesis[len(llm.synthesis)-1].Content
	if strings.Contains(last, "Kutuzov") {
		t.Fatalf("a seat that failed has no position to set against: %q", last)
	}
	if !strings.Contains(llm.synthesis[0].Content, "Answer in Zhukov's frame") {
		t.Fatal("with one seat left the answer is that seat's frame, not an exchange")
	}
	// The failed general stays seated for the follow-up.
	if strings.Join(result.Generals, ",") != "zhukov,kutuzov" {
		t.Fatalf("generals = %v", result.Generals)
	}
}

func TestCouncilFailsWhenNobodyReports(t *testing.T) {
	t.Parallel()

	llm := &councilLLM{fail: map[string]bool{"Kutuzov": true, "Zhukov": true}}
	council := NewCouncil(llm, nil, stubInitialContext{})

	if _, err := council.Execute(context.Background(), councilPlan(), nil); err == nil {
		t.Fatal("expected an error")
	}
	if llm.synthesis != nil {
		t.Fatal("no closing call without positions")
	}
}

func TestUsesCouncilOnlyForTheOrdinaryExchangeAtDepth(t *testing.T) {
	t.Parallel()

	interrogation := modedomain.Mode{ID: "interrogation", Family: modedomain.FamilyInterrogate}
	beast := modedomain.Mode{ID: "beast", Family: modedomain.FamilyBeast}

	cases := []struct {
		name string
		edit func(*ConsultPlan)
		want bool
	}{
		{"deep with two generals", func(*ConsultPlan) {}, true},
		{"standard tier", func(p *ConsultPlan) { p.Tier = domain.PolicyFor(domain.TierStandard) }, false},
		{"one general", func(p *ConsultPlan) { p.Generals = p.Generals[:1] }, false},
		{"plain check-in", func(p *ConsultPlan) { p.Plain = true }, false},
		{"closing turn", func(p *ConsultPlan) { p.Conclude = true }, false},
		{"interrogation", func(p *ConsultPlan) { p.Mode = interrogation }, false},
		{"beast", func(p *ConsultPlan) { p.Mode = beast }, false},
	}
	for _, tc := range cases {
		plan := councilPlan()
		tc.edit(&plan)
		if got := plan.UsesCouncil(); got != tc.want {
			t.Errorf("%s: UsesCouncil = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Lenses lead, but never into an obvious loss: every prompt a general speaks
// through carries the same instruction, so one mode cannot quietly drop it.
func TestEveryGeneralPromptWeighsDoctrineAgainstReality(t *testing.T) {
	t.Parallel()

	plan := councilPlan()
	prompts := map[string]string{
		"single call":   generalsPrompt(plan.Generals, plan.Doctrine, false, false),
		"council seat":  seatPrompt(plan, plan.Generals[0]),
		"interrogation": interrogationPrompt(plan.Generals, plan.Doctrine, nil, nil, false),
		"beast":         beastPrompt(plan.Generals, []string{"zhukov"}, plan.Doctrine, nil, false),
	}
	for name, prompt := range prompts {
		if !strings.Contains(prompt, "against reality first") {
			t.Errorf("%s prompt is missing the lens judgement instruction", name)
		}
	}
	if seat := prompts["council seat"]; !strings.Contains(seat, "**What it costs:**") {
		t.Error("a council seat must state what its own move costs")
	}
	if !strings.Contains(councilPrompt(plan.Generals, false, true), "What it costs") {
		t.Error("the closing call must keep each seat's cost")
	}
}

// A move comes with its forecast wherever there is room for one: standard and
// deep, in a council seat and in the exchange. Quick's 120 words cannot hold it.
func TestForecastFollowsTheTier(t *testing.T) {
	t.Parallel()

	plan := councilPlan()
	if !domain.PolicyFor(domain.TierStandard).Forecast || !plan.Tier.Forecast || domain.PolicyFor(domain.TierQuick).Forecast {
		t.Fatal("forecast should be on for standard and deep, off for quick")
	}

	for name, prompt := range map[string]string{
		"exchange":       generalsPrompt(plan.Generals, plan.Doctrine, false, true),
		"structured":     generalsPrompt(plan.Generals, plan.Doctrine, true, true),
		"single lens":    generalsPrompt(plan.Generals[:1], plan.Doctrine, false, true),
		"council seat":   seatPrompt(plan, plan.Generals[0]),
		"council answer": councilPrompt(plan.Generals, false, true),
	} {
		if !strings.Contains(prompt, "up to five or six") || !strings.Contains(prompt, "up to three") {
			t.Errorf("%s prompt is missing the forecast", name)
		}
	}

	if strings.Contains(generalsPrompt(plan.Generals[:1], plan.Doctrine, false, false), "Where it goes worse") {
		t.Error("forecast leaked into a prompt that turned it off")
	}
}
