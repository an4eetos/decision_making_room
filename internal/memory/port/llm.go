package port

import "context"

type Message struct {
	Role      string
	Content   string
	ToolCalls []ToolCall
	ToolName  string
	// ToolCallID answers the call with the same ID. Providers that pair calls
	// with results by ID (Anthropic) need it; the others match by name.
	ToolCallID string
	Parts      []ContentPart
}

type ContentPart struct {
	Text             string
	ToolCall         *ToolCall
	ThoughtSignature string
	// Reasoning is a thinking block the model wrote before its text or tool
	// calls. It is only ever replayed to the provider that produced it, within
	// the same turn, and must go back unchanged or the next round is rejected.
	Reasoning *Reasoning
}

// Reasoning is one thinking block. Data is set instead of Thinking when the
// provider redacted it.
type Reasoning struct {
	Thinking  string
	Signature string
	Data      string
}

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type ToolCall struct {
	// ID is the provider's own handle for the call, empty when it has none.
	ID        string
	Name      string
	Arguments map[string]any
}

type ChatTurn struct {
	Content   string
	ToolCalls []ToolCall
	Parts     []ContentPart
}

type toolsWithdrawnKey struct{}

// WithToolsWithdrawn marks a tool round that must be answered without calling
// anything: the last round of an agent loop. The caller still passes the same
// tools. Providers that check the tool set against the history they already
// saw (Anthropic, whose thinking blocks are bound to it) keep the tools and
// forbid calls; the others simply drop them.
func WithToolsWithdrawn(ctx context.Context) context.Context {
	return context.WithValue(ctx, toolsWithdrawnKey{}, true)
}

// ToolsWithdrawn reports whether WithToolsWithdrawn was applied.
func ToolsWithdrawn(ctx context.Context) bool {
	v, _ := ctx.Value(toolsWithdrawnKey{}).(bool)
	return v
}

type noticeKey struct{}

// WithNotice lets a provider explain a wait it cannot avoid: a busy endpoint it
// is retrying, a quota it is waiting out, a switch to the fallback model. The
// waiting line otherwise says "thinking" through a minute of retries. fn may be
// called from any goroutine.
func WithNotice(ctx context.Context, fn func(string)) context.Context {
	return context.WithValue(ctx, noticeKey{}, fn)
}

// Notice reports msg to whoever asked through WithNotice, if anyone did.
func Notice(ctx context.Context, msg string) {
	if fn, ok := ctx.Value(noticeKey{}).(func(string)); ok && fn != nil {
		fn(msg)
	}
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
