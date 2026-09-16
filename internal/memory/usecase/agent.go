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
		aboutMe, plan.History,
		formatContext(prefetch, plan.Tier.MaxBodyRunes),
		plan.Question,
	)

	toolDefs := MemoryTools()
	seenCallBatches := make(map[string]int)

	// MaxToolRounds counts rounds where tools are offered; the extra iteration is
	// the same conversation with tools withdrawn, so the model answers from what
	// it gathered instead of being cut off mid-dig.
	totalRounds := plan.Tier.MaxToolRounds + 1

	for round := 0; round < totalRounds; round++ {
		lastRound := round == totalRounds-1

		toolsForRound := toolDefs
		if lastRound {
			toolsForRound = nil
		}

		turn, err := a.toolLLM.ChatTools(ctx, messages, toolsForRound)
		if err != nil {
			return ConsultResult{}, fmt.Errorf("agent chat round %d: %w", round+1, err)
		}

		if len(turn.ToolCalls) == 0 {
			if strings.TrimSpace(turn.Content) == "" {
				return ConsultResult{}, fmt.Errorf("model returned empty response")
			}
			return ConsultResult{
				Answer:         turn.Content,
				Sources:        entriesToSources(collected),
				Tier:           string(plan.Tier.Tier),
				Generals:       plan.GeneralIDs(),
				GeneralsMethod: plan.GeneralsMethod,
				Mode:           plan.Mode.ID,
				ModeName:       plan.Mode.Name,
				ModeMethod:     plan.ModeMethod,
			}, nil
		}

		if lastRound {
			break
		}

		batchKey := toolCallBatchKey(turn.ToolCalls)
		seenCallBatches[batchKey]++
		if batchKey != "" && seenCallBatches[batchKey] > 1 {
			break
		}

		messages = append(messages, port.Message{
			Role:      "assistant",
			Content:   turn.Content,
			ToolCalls: turn.ToolCalls,
			Parts:     turn.Parts,
		})

		for _, call := range turn.ToolCalls {
			result, err := a.tools.Execute(ctx, call.Name, call.Arguments, plan.Tier)
			if err != nil {
				result = ToolExecutionResult{Content: "tool error: " + err.Error()}
			}
			collected = mergeSourceEntries(collected, result.Entries)
			messages = append(messages, port.Message{
				Role:     "tool",
				ToolName: call.Name,
				Content:  result.Content,
			})
		}
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
		aboutMe, plan.History,
		formatContext(collected, plan.Tier.MaxBodyRunes),
		plan.Question,
	)
	answer, err := a.toolLLM.Chat(ctx, finalMessages)
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
		Mode:           plan.Mode.ID,
		ModeName:       plan.Mode.Name,
		ModeMethod:     plan.ModeMethod,
	}, nil
}

func toolCallBatchKey(calls []port.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}

	var b strings.Builder
	for _, call := range calls {
		b.WriteString(call.Name)
		b.WriteString(":")
		b.WriteString(fmt.Sprint(call.Arguments))
		b.WriteString("|")
	}
	return b.String()
}
