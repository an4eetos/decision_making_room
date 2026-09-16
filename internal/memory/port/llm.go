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
