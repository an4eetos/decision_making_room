package usecase

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// StageCouncil is the generals working as separate agents. Its detail names who
// is still out, so a deep answer's long silence says what it is waiting on.
const StageCouncil = "council"

const (
	// councilSeatRounds caps each general's own digging. Three generals at the
	// deep tier's full three rounds would be twelve calls before a word is
	// written; two each keeps the council near the single agent's cost per seat.
	councilSeatRounds = 2
	// councilHeartbeat re-sends the council stage while generals are out. A seat
	// that thinks for a minute without calling a tool sends nothing, and the
	// browser drops a stream that has been silent for ninety seconds.
	councilHeartbeat = 20 * time.Second
)

// Council runs each seated general as its own agent: its own card and doctrine,
// its own digging through memory, its position written without seeing the
// others'. One closing call then writes the exchange from those positions.
//
// This is what the single call cannot do. One model playing three lenses at
// once averages them — they share one pass over the notes and one line of
// reasoning, and the disagreement is staged. Separate seats can find different
// evidence and genuinely reach different calls, and the closing call only has
// to set what they actually said against each other.
type Council struct {
	llm            port.ToolLLM
	tools          *MemoryToolExecutor
	initialContext port.InitialContextReader
}

func NewCouncil(llm port.ToolLLM, tools *MemoryToolExecutor, initialContext port.InitialContextReader) *Council {
	return &Council{llm: llm, tools: tools, initialContext: initialContext}
}

// councilPosition is one seat's report.
type councilPosition struct {
	lens  gendomain.Lens
	text  string
	found []domain.MemoryEntry
	err   error
}

func (c *Council) Execute(ctx context.Context, plan ConsultPlan, prefetch []domain.MemoryEntry) (ConsultResult, error) {
	aboutMe, err := c.initialContext.Read()
	if err != nil {
		return ConsultResult{}, fmt.Errorf("read about me: %w", err)
	}

	positions := c.convene(ctx, plan, aboutMe, prefetch)
	if err := ctx.Err(); err != nil {
		return ConsultResult{}, err
	}

	collected := append([]domain.MemoryEntry(nil), prefetch...)
	var reported []councilPosition
	var firstErr error
	for _, p := range positions {
		if p.err != nil {
			// One seat failing should not cost the answer: the others still
			// argue, and the exchange is written between those who reported.
			log.Printf("council: %s did not report: %v", p.lens.ID, p.err)
			if firstErr == nil {
				firstErr = p.err
			}
			continue
		}
		reported = append(reported, p)
		collected = mergeSourceEntries(collected, p.found)
	}
	if len(reported) == 0 {
		return ConsultResult{}, fmt.Errorf("no general reported: %w", firstErr)
	}

	answer, err := c.synthesise(ctx, plan, aboutMe, collected, reported)
	if err != nil {
		return ConsultResult{}, err
	}

	return ConsultResult{
		Answer:  answer,
		Sources: entriesToSources(collected),
		Tier:    string(plan.Tier.Tier),
		// Every seated general, including one that failed to report, so the
		// roster stays seated for the follow-up instead of shrinking on a
		// timeout.
		Generals:       plan.GeneralIDs(),
		GeneralsMethod: plan.GeneralsMethod,
		Doctrine:       plan.DoctrineRefs(),
		Mode:           plan.Mode.ID,
		ModeName:       plan.Mode.Name,
		ModeMethod:     plan.ModeMethod,
	}, nil
}

// convene runs every seat at once and waits for all of them. Progress hooks
// write to one HTTP stream, so every stage report from a seat goes through one
// lock.
func (c *Council) convene(ctx context.Context, plan ConsultPlan, aboutMe string, prefetch []domain.MemoryEntry) []councilPosition {
	var (
		mu      sync.Mutex
		pending = make(map[string]bool, len(plan.Generals))
		detail  string
	)
	for _, lens := range plan.Generals {
		pending[lens.ID] = true
	}
	report := func(text string) {
		mu.Lock()
		defer mu.Unlock()
		detail = text
		plan.Progress.stage(StageCouncil, text)
	}
	stillOut := func() string {
		var names []string
		for _, lens := range plan.Generals {
			if pending[lens.ID] {
				names = append(names, lens.Name)
			}
		}
		return joinNames(names)
	}

	report(stillOut() + " are each working it out on their own")

	// The heartbeat must be gone before convene returns: the closing call
	// streams on the same connection, outside the lock.
	stop := make(chan struct{})
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		ticker := time.NewTicker(councilHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				plan.Progress.stage(StageCouncil, detail)
				mu.Unlock()
			}
		}
	}()

	positions := make([]councilPosition, len(plan.Generals))
	var wg sync.WaitGroup
	for i, lens := range plan.Generals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			onCall := func(call port.ToolCall) {
				if d := toolStageDetail(call); d != "" {
					report(lens.Name + ": " + d)
				}
			}
			positions[i] = c.seat(ctx, plan, lens, aboutMe, prefetch, onCall)

			mu.Lock()
			delete(pending, lens.ID)
			out := stillOut()
			mu.Unlock()
			if out != "" {
				report(lens.Name + " has reported. Waiting on " + out)
			}
		}()
	}
	wg.Wait()
	close(stop)
	<-beatDone

	return positions
}

// seat is one general's agent. It sees what the consult already retrieved and
// may dig further, and read the rest of its own doctrine, but never another
// general's.
func (c *Council) seat(
	ctx context.Context,
	plan ConsultPlan,
	lens gendomain.Lens,
	aboutMe string,
	prefetch []domain.MemoryEntry,
	onCall func(port.ToolCall),
) councilPosition {
	messages := buildConsultMessages(
		seatPrompt(plan, lens),
		aboutMe, plan.Situation, plan.History,
		formatContext(prefetch, plan.Tier.MaxBodyRunes),
		plan.Question,
	)

	toolDefs := MemoryTools()
	if plan.Tier.ReadDoctrine {
		toolDefs = append(toolDefs, ReadDoctrineTool([]string{lens.ID}))
	}

	rounds := plan.Tier.MaxToolRounds
	if rounds > councilSeatRounds {
		rounds = councilSeatRounds
	}
	loop := toolLoop{
		llm:    c.llm,
		tools:  c.tools,
		rounds: rounds,
		policy: plan.Tier,
		onCall: onCall,
	}

	text, found, err := loop.run(ctx, messages, toolDefs)
	if err == nil && strings.TrimSpace(text) == "" {
		err = fmt.Errorf("stopped digging without a position")
	}
	return councilPosition{lens: lens, text: strings.TrimSpace(text), found: found, err: err}
}

// synthesise writes the exchange. It gets no tools: the digging is done, and
// what the generals found is in front of it as their positions and sources.
func (c *Council) synthesise(
	ctx context.Context,
	plan ConsultPlan,
	aboutMe string,
	collected []domain.MemoryEntry,
	reported []councilPosition,
) (string, error) {
	lenses := make([]gendomain.Lens, len(reported))
	for i, p := range reported {
		lenses[i] = p.lens
	}
	plan.Generals = lenses
	plan.Council = true

	messages := buildConsultMessages(
		buildSystemPrompt(systemPrompt, plan),
		aboutMe, plan.Situation, plan.History,
		formatContext(collected, plan.Tier.MaxBodyRunes),
		plan.Question,
	)
	// The positions are model output, so they travel as data in the person's
	// turn rather than as instructions in the system prompt.
	messages = append(messages, port.Message{Role: "user", Content: positionsBlock(reported)})

	plan.Progress.stage(StageThinking, "")
	answer, err := streamChat(ctx, c.llm, messages, plan.Progress)
	if err != nil {
		return "", fmt.Errorf("council synthesis: %w", err)
	}
	if strings.TrimSpace(answer) == "" {
		return "", fmt.Errorf("council synthesis returned an empty answer")
	}
	return answer, nil
}

const seatBase = `You advise one person, using their stored decisions, plans and notes.
Relevant memories are pre-loaded under "Retrieved context". Do not invent facts about them.

You have recall_memories for a topic the pre-loaded context clearly lacks, and read_doctrine for the rest of your own doctrine. Prefer answering without them; at most two calls, never a repeated query.`

// seatPrompt is one general's brief. It names the other seats so the general
// can aim at the objection it expects, but it never sees what they wrote.
func seatPrompt(plan ConsultPlan, lens gendomain.Lens) string {
	var b strings.Builder
	b.WriteString(seatBase)
	b.WriteString("\n\n")

	// The mode's situation, not its template: the template belongs to the
	// closing call, which lays the exchange out inside it.
	if summary := strings.TrimSpace(plan.Mode.Summary); summary != "" && strings.TrimSpace(plan.Mode.SystemPrompt) != "" {
		fmt.Fprintf(&b, "The conversation is a %s: %s\n\n", strings.ToLower(plan.Mode.Name), summary)
	}

	fmt.Fprintf(&b, "You hold one seat on a council, as the %s lens. ", lens.Name)
	var others []string
	for _, other := range plan.Generals {
		if other.ID != lens.ID {
			others = append(others, fmt.Sprintf("%s (%s)", other.Name, other.Job))
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "The other seats are %s. ", joinNames(others))
	}
	b.WriteString("Each seat argues alone and none sees the others' positions; " +
		"afterwards they are set against each other for the person. " +
		"So argue from your lens, not toward the others: a seat that meets the other lenses halfway " +
		"has left nothing to argue with. That is not the same as ignoring reality, which you may never do.\n\n")
	b.WriteString(seatJudgement)
	b.WriteString("\n\n")

	b.WriteString("Use the lens as a frame, not a character: no period voice, no invented biography or quotes. " +
		"Keep the register of its \"Sounds like\" line and speak to the person directly. ")
	if len(plan.Doctrine[lens.ID]) > 0 {
		b.WriteString("Argue from the doctrine passages under the card: they are this lens's actual position " +
			"on this kind of situation, and they outrank anything you know about the historical figure.")
	}
	b.WriteString("\n\n")

	b.WriteString(lensBlocks([]gendomain.Lens{lens}, plan.Doctrine))
	b.WriteString("\n\n")

	if plan.Tier.Forecast {
		b.WriteString("Forecast your own move as well. The others will use your \"Where it goes worse\" against you, " +
			"so find the real ones before they do.\n")
		b.WriteString(forecastRules("forecast"))
		b.WriteString("\n\n")
	}

	b.WriteString("Write your position exactly like this, under 450 words, no preamble:\n\n")
	b.WriteString("**Position:** one line.\n")
	b.WriteString("The advice, concretely, about this question. Name what in their notes it rests on.\n")
	b.WriteString("**What it costs:** the loss your own move brings here, plainly, and why it is still worth it, " +
		"or how you changed the move because it is not.\n")
	b.WriteString("**First move:** what they do first, and when.\n")
	if plan.Tier.Forecast {
		b.WriteString("**Unexpected wins:** up to three, one line each.\n")
		b.WriteString("**Where it goes worse:** up to five or six, one line each, with the early sign.\n")
	}
	b.WriteString("**Against the strongest objection:** the best case another seat will make against you, and your answer to it.\n")
	b.WriteString("**Would change my mind:** the fact or condition that would, from your \"concedes when\".\n")
	b.WriteString("**Blind spot here:** one line, only if your blind spot bears on this question.")
	return b.String()
}

// councilPrompt is the closing call's lens block. The cards are there so it
// knows each lens's job and blind spot; the doctrine is not, because the seats
// already argued from it.
func councilPrompt(lenses []gendomain.Lens, structured, forecast bool) string {
	if len(lenses) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("These planning lenses have each argued this question as a separate adviser, " +
		"with their own reading of the notes, without seeing each other. " +
		"Their positions are in the last message. Write the answer from them.\n")
	b.WriteString("Keep each position as the lens took it. Do not invent a position a lens did not take, " +
		"do not move a lens toward the others, and do not soften its register: a blunt lens stays blunt " +
		"and speaks to the person directly. The rebuttals and concessions are yours to draw out, " +
		"from what each lens actually wrote. Where a lens grounded a point in the person's notes, keep that. " +
		"Keep each lens's \"What it costs\" too: the call is weighed against what each move really costs, " +
		"not against how well it fits its doctrine.\n")
	if forecast {
		b.WriteString("Keep every seat's forecast, its unexpected wins and where it goes worse, item for item: " +
			"tighten the wording if you must, but do not drop, merge or invent items.\n")
	}
	b.WriteString("Use them as frames, not characters: no period voice, no invented biography.\n\n")

	cards := make([]string, len(lenses))
	for i, lens := range lenses {
		cards[i] = lens.Card()
	}
	b.WriteString(strings.Join(cards, "\n\n"))
	b.WriteString("\n\n")
	b.WriteString(exchangeShape(lenses, structured, forecast))
	return b.String()
}

func positionsBlock(reported []councilPosition) string {
	var b strings.Builder
	b.WriteString("The council's positions, each written alone:")
	for _, p := range reported {
		fmt.Fprintf(&b, "\n\n### %s\n%s", p.lens.Name, p.text)
	}
	return b.String()
}

// joinNames reads "A", "A and B", "A, B and C".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}
