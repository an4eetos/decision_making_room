package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

const agentSystemPrompt = `You are a personal advisor with access to the user's stored decisions, plans, and notes.

Relevant memories are pre-loaded in the user message under "Pre-loaded memories".
Answer directly from that context when it is sufficient.

You have one optional tool: recall_memories — use it ONLY when pre-loaded context clearly lacks a specific topic.
Rules:
- Prefer answering without tools
- At most two recall_memories calls per question
- Never repeat a similar query
- If context is empty or partial, answer with what you have instead of looping

Do not invent facts. Be concise and actionable.`

type AgentConsult struct {
	toolLLM        port.ToolLLM
	tools          *MemoryToolExecutor
	initialContext port.InitialContextReader
	maxRounds      int
}

func NewAgentConsult(
	toolLLM port.ToolLLM,
	tools *MemoryToolExecutor,
	initialContext port.InitialContextReader,
	maxRounds int,
) *AgentConsult {
	if maxRounds <= 0 {
		maxRounds = 3
	}
	return &AgentConsult{
		toolLLM:        toolLLM,
		tools:          tools,
		initialContext: initialContext,
		maxRounds:      maxRounds,
	}
}

func (a *AgentConsult) Execute(ctx context.Context, input ConsultInput, prefetch []domain.MemoryEntry) (ConsultResult, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" {
		return ConsultResult{}, fmt.Errorf("question is required")
	}

	aboutMe, err := a.initialContext.Read()
	if err != nil {
		return ConsultResult{}, fmt.Errorf("read about me: %w", err)
	}

	collected := append([]domain.MemoryEntry(nil), prefetch...)
	contextBlock := formatContext(prefetch)
	messages := buildConsultMessages(agentSystemPrompt, aboutMe, input.History, contextBlock, question)

	toolDefs := MemoryTools()
	seenCallBatches := make(map[string]int)

	for round := 0; round < a.maxRounds; round++ {
		toolsForRound := toolDefs
		if round == a.maxRounds-1 {
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
				Answer:  turn.Content,
				Sources: entriesToSources(collected),
			}, nil
		}

		if round == a.maxRounds-1 {
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
			result, err := a.tools.Execute(ctx, call.Name, call.Arguments)
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

	return a.fallbackAnswer(ctx, aboutMe, input, collected)
}

func (a *AgentConsult) fallbackAnswer(
	ctx context.Context,
	aboutMe string,
	input ConsultInput,
	collected []domain.MemoryEntry,
) (ConsultResult, error) {
	contextBlock := formatContext(collected)
	finalMessages := buildConsultMessages(systemPrompt, aboutMe, input.History, contextBlock, input.Question)
	answer, err := a.toolLLM.Chat(ctx, finalMessages)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("agent fallback llm chat: %w", err)
	}
	if strings.TrimSpace(answer) == "" {
		return ConsultResult{}, fmt.Errorf("agent could not produce an answer")
	}
	return ConsultResult{
		Answer:  answer,
		Sources: entriesToSources(collected),
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
