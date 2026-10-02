package usecase

import (
	"context"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

// Stages a consult reports as it reaches them. They name what the server is
// actually doing, so the waiting text can stop guessing from a timer.
const (
	StageSearching = "searching"
	StageReading   = "reading"
	StageDigging   = "digging"
	StageThinking  = "thinking"
)

// PlanInfo is what is known about an answer before any of it is written: the
// depth that will run, the lenses chosen and the mode detected. Sent early so
// the interface can name the lenses while it waits instead of after.
type PlanInfo struct {
	Tier           string   `json:"tier"`
	Generals       []string `json:"generals"`
	GeneralsMethod string   `json:"generals_method,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	ModeMethod     string   `json:"mode_method,omitempty"`
}

// Progress hears about a consult while it runs. Every hook is optional and a nil
// *Progress is valid, so the non-streaming path passes nothing and pays nothing.
//
// Hooks are called synchronously from the request goroutine and must return
// quickly: a slow hook stalls the model stream behind it.
type Progress struct {
	OnStage func(stage, detail string)
	OnPlan  func(PlanInfo)
	OnDelta func(text string)
	// OnReset discards text already delivered. A tool-using round can write a
	// sentence and then decide to call a tool instead; that sentence is not the
	// answer and must not stay on screen.
	OnReset func()
	// OnAnswer receives the stored answer as soon as it is saved. The chat goes
	// on to summarise afterwards, which is another model call; the answer should
	// not have to wait for it to look finished.
	OnAnswer func(ChatMessageDTO)
}

func (p *Progress) stage(stage, detail string) {
	if p != nil && p.OnStage != nil {
		p.OnStage(stage, detail)
	}
}

func (p *Progress) plan(info PlanInfo) {
	if p != nil && p.OnPlan != nil {
		p.OnPlan(info)
	}
}

func (p *Progress) answer(message ChatMessageDTO) {
	if p != nil && p.OnAnswer != nil {
		p.OnAnswer(message)
	}
}

func (p *Progress) streaming() bool {
	return p != nil && p.OnDelta != nil
}

func (p *Progress) reset() {
	if p != nil && p.OnReset != nil {
		p.OnReset()
	}
}

// deltaSink wraps OnDelta and remembers whether anything was delivered, so a
// round that turns out to be a tool call knows whether it needs a reset.
type deltaSink struct {
	progress *Progress
	sent     bool
}

func (s *deltaSink) write(text string) {
	if text == "" {
		return
	}
	s.sent = true
	s.progress.OnDelta(text)
}

// streamChat answers through the streaming call when both the caller wants a
// stream and the client can produce one, and through the plain call otherwise.
func streamChat(ctx context.Context, llm port.LLM, messages []port.Message, progress *Progress) (string, error) {
	streamer, ok := llm.(port.StreamingLLM)
	if !ok || !progress.streaming() {
		return llm.Chat(ctx, messages)
	}
	sink := &deltaSink{progress: progress}
	return streamer.ChatStream(ctx, messages, sink.write)
}

// streamChatTools is streamChat for a tool round. Text streamed before the model
// settles on tool calls is retracted with a reset.
func streamChatTools(ctx context.Context, llm port.ToolLLM, messages []port.Message, tools []port.Tool, progress *Progress) (port.ChatTurn, error) {
	streamer, ok := llm.(port.StreamingLLM)
	if !ok || !progress.streaming() {
		return llm.ChatTools(ctx, messages, tools)
	}
	sink := &deltaSink{progress: progress}
	turn, err := streamer.ChatToolsStream(ctx, messages, tools, sink.write)
	if sink.sent && (err != nil || len(turn.ToolCalls) > 0) {
		progress.reset()
	}
	return turn, err
}

// toolStageDetail describes a tool call in words, for the digging stage.
func toolStageDetail(call port.ToolCall) string {
	switch call.Name {
	case "recall_memories":
		if q, ok := call.Arguments["query"].(string); ok && strings.TrimSpace(q) != "" {
			return "Searching again: " + strings.TrimSpace(q)
		}
		return "Searching again"
	case "read_doctrine":
		return "Reading doctrine"
	default:
		return ""
	}
}
