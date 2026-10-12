package usecase

import (
	"context"
	"fmt"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// toolLoop is one agent digging through memory: rounds of tool calls, then an
// answer. The consult's agent runs it for the answer itself; the council runs
// one per general.
type toolLoop struct {
	llm   port.ToolLLM
	tools *MemoryToolExecutor
	// rounds counts rounds where tools may be called. The loop always gets one
	// further round with them withdrawn, so the model answers from what it
	// gathered instead of being cut off mid-dig.
	rounds int
	policy domain.TierPolicy
	// progress streams each round and hears the thinking stage. Nil runs
	// silently, as a council seat does.
	progress *Progress
	// onCall is told about each tool call before it runs. Optional.
	onCall func(port.ToolCall)
}

// run returns the answer and the memories the tools turned up. An empty answer
// with no error means the loop stopped without one, out of rounds or caught
// repeating itself; the caller decides how to answer instead.
func (l toolLoop) run(ctx context.Context, messages []port.Message, toolDefs []port.Tool) (string, []domain.MemoryEntry, error) {
	var collected []domain.MemoryEntry
	seenCallBatches := make(map[string]int)
	totalRounds := l.rounds + 1

	for round := 0; round < totalRounds; round++ {
		lastRound := round == totalRounds-1

		// The tools stay declared on the last round, only calling them is
		// withdrawn; see port.WithToolsWithdrawn.
		roundCtx := ctx
		if lastRound {
			roundCtx = port.WithToolsWithdrawn(ctx)
		}

		// Every round streams: the one that answers is only known once it ends,
		// and a round that turns into tool calls retracts what it wrote.
		l.progress.stage(StageThinking, "")
		turn, err := streamChatTools(roundCtx, l.llm, messages, toolDefs, l.progress)
		if err != nil {
			return "", collected, fmt.Errorf("chat round %d: %w", round+1, err)
		}

		if len(turn.ToolCalls) == 0 {
			if strings.TrimSpace(turn.Content) == "" {
				return "", collected, fmt.Errorf("model returned empty response")
			}
			return turn.Content, collected, nil
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
			if l.onCall != nil {
				l.onCall(call)
			}
			result, err := l.tools.Execute(ctx, call.Name, call.Arguments, l.policy)
			if err != nil {
				result = ToolExecutionResult{Content: "tool error: " + err.Error()}
			}
			collected = mergeSourceEntries(collected, result.Entries)
			messages = append(messages, port.Message{
				Role:       "tool",
				ToolName:   call.Name,
				ToolCallID: call.ID,
				Content:    result.Content,
			})
		}
	}

	return "", collected, nil
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
