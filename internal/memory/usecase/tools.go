package usecase

import "github.com/an4eetos/decision-room/internal/memory/port"

func MemoryTools() []port.Tool {
	return []port.Tool{
		{
			Name: "recall_memories",
			Description: "Search stored notes, decisions, plans, and journal entries. " +
				"Relevant memories are already pre-loaded in the prompt — call this only when a specific topic is clearly missing. " +
				"At most one call per question; do not repeat with similar queries.",
			Parameters: map[string]any{
				"type":     "object",
				"required": []string{"query"},
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Focused search query for the missing topic",
					},
					"include_recent": map[string]any{
						"type":        "boolean",
						"description": "Also include recent entries (default true)",
					},
					"kind": map[string]any{
						"type":        "string",
						"description": "Optional filter: decision, plan, note, daily_log",
						"enum":        []string{"decision", "plan", "note", "daily_log"},
					},
					"tags": map[string]any{
						"type":        "array",
						"description": "Optional tag filters, e.g. workout, book, daily",
						"items":       map[string]any{"type": "string"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Max results (default 6, max 8)",
					},
				},
			},
		},
	}
}
