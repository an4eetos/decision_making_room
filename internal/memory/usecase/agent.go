package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

const agentSystemPrompt = `You are a personal advisor with access to the user's stored decisions, plans, and notes.

Relevant memories are pre-loaded in the user message under "Retrieved context".
Answer directly from that context when it is sufficient.

You have one optional tool: recall_memories — use it ONLY when pre-loaded context clearly lacks a specific topic.
Rules:
- Prefer answering without tools
- At most two recall_memories calls per question
- Never repeat a similar query
- If context is empty or partial, answer with what you have instead of looping

Do not invent facts. Be concise and actionable.`

// AgentConsult holds no per-request state. The round limit in particular lives
// on the plan, not here: as a struct field it was frozen when the dependency
// graph was built, which made a per-request depth setting impossible.
type AgentConsult struct {
	toolLLM        port.ToolLLM
	tools          *MemoryToolExecutor
	initialContext port.InitialContextReader
}

func NewAgentConsult(
	toolLLM port.ToolLLM,
	tools *MemoryToolExecutor,
	initialContext port.InitialContextReader,
) *AgentConsult {
	return &AgentConsult{
		toolLLM:        toolLLM,
		tools:          tools,
		initialContext: initialContext,
	}
}

func (a *AgentConsult) Execute(ctx context.Context, plan ConsultPlan, prefetch []domain.MemoryEntry) (ConsultResult, error) {
	if plan.Question == "" {
		return ConsultResult{}, fmt.Errorf("question is required")
	}

	aboutMe, err := a.initialContext.Read()
	if err != nil {
		return ConsultResult{}, fmt.Errorf("read about me: %w", err)
	}

	collected := append([]domain.MemoryEntry(nil), prefetch...)
	messages := buildConsultMessages(
		buildSystemPrompt(agentSystemPrompt, plan),
		aboutMe, plan.Situation, plan.History,
		formatContext(prefetch, plan.Tier.MaxBodyRunes),
		plan.Question,
	)

	toolDefs := MemoryTools()
	// The doctrine tool is only offered when there are lenses to read and the
	// tier can afford the extra round.
	if plan.Tier.ReadDoctrine && len(plan.Generals) > 0 {
		toolDefs = append(toolDefs, ReadDoctrineTool(plan.GeneralIDs()))
	}
	loop := toolLoop{
		llm:      a.toolLLM,
		tools:    a.tools,
		rounds:   plan.Tier.MaxToolRounds,
		policy:   plan.Tier,
		progress: plan.Progress,
		onCall: func(call port.ToolCall) {
			plan.Progress.stage(StageDigging, toolStageDetail(call))
		},
	}
	answer, found, err := loop.run(ctx, messages, toolDefs)
	collected = mergeSourceEntries(collected, found)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("agent %w", err)
	}
	if answer != "" {
		return ConsultResult{
			Answer:         answer,
			Sources:        entriesToSources(collected),
			Tier:           string(plan.Tier.Tier),
			Generals:       plan.GeneralIDs(),
			GeneralsMethod: plan.GeneralsMethod,
			Doctrine:       plan.DoctrineRefs(),
			Mode:           plan.Mode.ID,
			ModeName:       plan.Mode.Name,
			ModeMethod:     plan.ModeMethod,
		}, nil
	}

	return a.fallbackAnswer(ctx, aboutMe, plan, collected)
}

// fallbackAnswer runs when the loop stopped because it ran out of rounds or hit
// the repeat guard. It switches to the non-agentic prompt deliberately: the
// agent prompt talks about tools that are no longer on offer.
func (a *AgentConsult) fallbackAnswer(
	ctx context.Context,
	aboutMe string,
	plan ConsultPlan,
	collected []domain.MemoryEntry,
) (ConsultResult, error) {
	finalMessages := buildConsultMessages(
		buildSystemPrompt(systemPrompt, plan),
		aboutMe, plan.Situation, plan.History,
		formatContext(collected, plan.Tier.MaxBodyRunes),
		plan.Question,
	)
	plan.Progress.stage(StageThinking, "")
	answer, err := streamChat(ctx, a.toolLLM, finalMessages, plan.Progress)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("agent fallback llm chat: %w", err)
	}
	if strings.TrimSpace(answer) == "" {
		return ConsultResult{}, fmt.Errorf("agent could not produce an answer")
	}
	return ConsultResult{
		Answer:         answer,
		Sources:        entriesToSources(collected),
		Tier:           string(plan.Tier.Tier),
		Generals:       plan.GeneralIDs(),
		GeneralsMethod: plan.GeneralsMethod,
		Doctrine:       plan.DoctrineRefs(),
		Mode:           plan.Mode.ID,
		ModeName:       plan.Mode.Name,
		ModeMethod:     plan.ModeMethod,
	}, nil
}
