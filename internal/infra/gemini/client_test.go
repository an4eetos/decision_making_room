package gemini

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

func TestToWireContentsMapsRoles(t *testing.T) {
	t.Parallel()

	system, contents, err := toWireContents([]port.Message{
		{Role: "system", Content: "be helpful"},
		{Role: "user", Content: "hello"},
		{
			Role:    "assistant",
			Content: "checking",
			ToolCalls: []port.ToolCall{
				{Name: "search_memories", Arguments: map[string]any{"query": "bench"}},
			},
		},
		{Role: "tool", ToolName: "search_memories", Content: "bench 60kg"},
	})
	if err != nil {
		t.Fatalf("toWireContents: %v", err)
	}

	if system == nil || system.Parts[0].Text != "be helpful" {
		t.Fatalf("system instruction = %#v", system)
	}
	if len(contents) != 3 {
		t.Fatalf("contents len = %d", len(contents))
	}
	if contents[0].Role != "user" || contents[0].Parts[0].Text != "hello" {
		t.Fatalf("user content = %#v", contents[0])
	}
	if contents[1].Role != "model" || contents[1].Parts[1].FunctionCall.Name != "search_memories" {
		t.Fatalf("assistant content = %#v", contents[1])
	}
	if contents[2].Parts[0].FunctionResponse.Name != "search_memories" {
		t.Fatalf("tool content = %#v", contents[2])
	}
}

func TestNormalizeSchemaUppercasesTypes(t *testing.T) {
	t.Parallel()

	schema := normalizeSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	})

	if schema["type"] != "OBJECT" {
		t.Fatalf("type = %v", schema["type"])
	}

	props := schema["properties"].(map[string]any)
	query := props["query"].(map[string]any)
	if query["type"] != "STRING" {
		t.Fatalf("query type = %v", query["type"])
	}
}

func TestParseModelTurn(t *testing.T) {
	t.Parallel()

	turn, err := parseModelTurn([]partWire{
		{Text: "answer "},
		{FunctionCall: &functionCallWire{Name: "search_memories", Args: map[string]any{"query": "gym"}}},
	})
	if err != nil {
		t.Fatalf("parseModelTurn: %v", err)
	}
	if turn.Content != "answer" {
		t.Fatalf("content = %q", turn.Content)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Name != "search_memories" {
		t.Fatalf("tool calls = %#v", turn.ToolCalls)
	}
	if len(turn.Parts) != 2 {
		t.Fatalf("parts = %#v", turn.Parts)
	}
}

func TestToWireContentsPreservesThoughtSignatures(t *testing.T) {
	t.Parallel()

	first := port.ToolCall{Name: "list_recent_memories", Arguments: map[string]any{"limit": 5}}
	second := port.ToolCall{Name: "search_memories", Arguments: map[string]any{"query": "today"}}

	_, contents, err := toWireContents([]port.Message{
		{
			Role: "assistant",
			Parts: []port.ContentPart{
				{ToolCall: &first, ThoughtSignature: "sig-1"},
				{ToolCall: &second},
			},
		},
	})
	if err != nil {
		t.Fatalf("toWireContents: %v", err)
	}

	if len(contents) != 1 || len(contents[0].Parts) != 2 {
		t.Fatalf("contents = %#v", contents)
	}
	if contents[0].Parts[0].ThoughtSignature != "sig-1" {
		t.Fatalf("first signature = %q", contents[0].Parts[0].ThoughtSignature)
	}
	if contents[0].Parts[1].ThoughtSignature != "" {
		t.Fatalf("second signature = %q", contents[0].Parts[1].ThoughtSignature)
	}
}

func TestNormalizeModel(t *testing.T) {
	t.Parallel()

	if got := normalizeModel("models/gemini-2.0-flash"); got != "gemini-2.0-flash" {
		t.Fatalf("normalizeModel = %q", got)
	}
}
