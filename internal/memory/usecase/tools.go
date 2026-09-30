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

// ReadDoctrineTool lets the model pull a general's full doctrine.
//
// Every tier already gets the passages most relevant to the question; this is
// for the rest, when the model needs a section that was not chosen, rather than
// everyone paying for all of it on every question.
// Offered at depth only, where an extra round is affordable.
func ReadDoctrineTool(ids []string) port.Tool {
	return port.Tool{
		Name: "read_doctrine",
		Description: "Read a planning lens's full doctrine when its summary card is not enough. " +
			"Only for lenses already assigned to this answer.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"general_id": map[string]any{
					"type":        "string",
					"description": "Which lens to read.",
					"enum":        ids,
				},
			},
			"required": []string{"general_id"},
		},
	}
}
