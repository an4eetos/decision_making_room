package port

import "context"

type Message struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
	ToolName  string
	Parts     []ContentPart
}

type ContentPart struct {
	Text             string
	ToolCall         *ToolCall
	ThoughtSignature string
}

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	Name      string
	Arguments map[string]any
}

type ChatTurn struct {
	Content   string
	ToolCalls []ToolCall
	Parts     []ContentPart
}

type LLM interface {
	Chat(ctx context.Context, messages []Message) (string, error)
}

type ToolLLM interface {
	LLM
	ChatTools(ctx context.Context, messages []Message, tools []Tool) (ChatTurn, error)
}

// StreamingLLM is implemented by clients that can deliver a turn while it is
// being written. onDelta receives each text fragment in order; the return value
// is the whole turn, the same as the non-streaming call would have produced.
// It is optional: callers type-assert for it and fall back to waiting.
type StreamingLLM interface {
	ChatStream(ctx context.Context, messages []Message, onDelta func(string)) (string, error)
	ChatToolsStream(ctx context.Context, messages []Message, tools []Tool, onDelta func(string)) (ChatTurn, error)
}
