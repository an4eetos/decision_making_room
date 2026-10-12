package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

// fakeAPI answers every request with the next scripted response and records
// what it was sent.
type fakeAPI struct {
	mu        sync.Mutex
	responses []fakeResponse
	requests  []map[string]any
	headers   []http.Header
}

type fakeResponse struct {
	status int
	sse    bool
	body   string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)

	f.mu.Lock()
	f.requests = append(f.requests, body)
	f.headers = append(f.headers, r.Header.Clone())
	resp := f.responses[0]
	if len(f.responses) > 1 {
		f.responses = f.responses[1:]
	}
	f.mu.Unlock()

	if resp.sse {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.status)
	_, _ = io.WriteString(w, resp.body)
}

func newTestClient(t *testing.T, api *fakeAPI, models ...string) *Client {
	t.Helper()
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	return newClient(models, "medium",
		option.WithBaseURL(server.URL),
		option.WithAPIKey("test-key"),
		option.WithMaxRetries(0),
	)
}

func sse(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		var probe struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(e), &probe)
		b.WriteString("event: " + probe.Type + "\ndata: " + e + "\n\n")
	}
	return b.String()
}

const messageStart = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":10,"output_tokens":0}}}`

func toolTurnStream() string {
	return sse(
		messageStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Let me "}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"check."}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_1","name":"recall_memories","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"query\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"visa\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":20}}`,
		`{"type":"message_stop"}`,
	)
}

func TestChatToolsStreamDeliversDeltasAndReplaysTheTurn(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{responses: []fakeResponse{
		{status: 200, sse: true, body: toolTurnStream()},
		{status: 200, body: `{"id":"msg_2","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Your visa runs out in May."}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":30,"output_tokens":8}}`},
	}}
	client := newTestClient(t, api, "claude-opus-5-5")

	tools := []port.Tool{{
		Name:        "recall_memories",
		Description: "Search the journal.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
			"required":   []any{"query"},
		},
	}}
	messages := []port.Message{
		{Role: "system", Content: "You are the room."},
		{Role: "user", Content: "About me: ..."},
		{Role: "user", Content: "When does my visa end?"},
	}

	var deltas []string
	turn, err := client.ChatToolsStream(context.Background(), messages, tools, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("ChatToolsStream: %v", err)
	}
	if strings.Join(deltas, "|") != "Let me |check." {
		t.Fatalf("deltas = %q", deltas)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].ID != "toolu_1" || turn.ToolCalls[0].Arguments["query"] != "visa" {
		t.Fatalf("tool calls = %#v", turn.ToolCalls)
	}
	if len(turn.Parts) != 3 || turn.Parts[0].Reasoning == nil || turn.Parts[0].Reasoning.Signature != "sig-1" {
		t.Fatalf("parts = %#v", turn.Parts)
	}

	first := api.requests[0]
	if first["fallbacks"] != "default" {
		t.Fatalf("fallbacks = %v", first["fallbacks"])
	}
	if beta := api.headers[0].Get("anthropic-beta"); !strings.Contains(beta, "server-side-fallback-2026-07-01") {
		t.Fatalf("anthropic-beta = %q", beta)
	}
	if got := first["output_config"].(map[string]any)["effort"]; got != "medium" {
		t.Fatalf("effort = %v", got)
	}
	// The context block and the question are one user turn, not two.
	if msgs := first["messages"].([]any); len(msgs) != 1 {
		t.Fatalf("first request messages = %d, want 1", len(msgs))
	}

	// The agent loop's next round: the assistant turn as returned, then the
	// tool's answer.
	messages = append(messages,
		port.Message{Role: "assistant", Content: turn.Content, ToolCalls: turn.ToolCalls, Parts: turn.Parts},
		port.Message{Role: "tool", ToolName: "recall_memories", ToolCallID: "toolu_1", Content: "visa expires 2027-05-01"},
	)
	answer, err := client.Chat(context.Background(), messages)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if answer != "Your visa runs out in May." {
		t.Fatalf("answer = %q", answer)
	}

	msgs := api.requests[1]["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("second request messages = %d, want 3", len(msgs))
	}
	assistant := msgs[1].(map[string]any)["content"].([]any)
	if kind := assistant[0].(map[string]any)["type"]; kind != "thinking" {
		t.Fatalf("assistant turn starts with %v, want the thinking block replayed first", kind)
	}
	if sig := assistant[0].(map[string]any)["signature"]; sig != "sig-1" {
		t.Fatalf("signature = %v", sig)
	}
	result := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool result = %#v", result)
	}
}

func TestParallelToolResultsShareOneUserTurn(t *testing.T) {
	t.Parallel()

	_, wire, err := toWireMessages([]port.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []port.ToolCall{{Name: "a"}, {Name: "b"}}},
		{Role: "tool", ToolName: "a", Content: "ra"},
		{Role: "tool", ToolName: "b", Content: "rb"},
	})
	if err != nil {
		t.Fatalf("toWireMessages: %v", err)
	}
	if len(wire) != 3 {
		t.Fatalf("turns = %d, want 3", len(wire))
	}
	raw, _ := json.Marshal(wire[2])
	// Calls with no provider ID get local ones, and results pair to them in order.
	if !strings.Contains(string(raw), `"tool_use_id":"toolu_local_0"`) || !strings.Contains(string(raw), `"tool_use_id":"toolu_local_1"`) {
		t.Fatalf("results = %s", raw)
	}
}

func TestRefusalIsAnError(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{responses: []fakeResponse{{status: 200, body: `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"bio","explanation":""},"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}`}}}
	client := newTestClient(t, api, "claude-opus-5-5")

	_, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "bio") {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamFailsOverWhenOverloaded(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{responses: []fakeResponse{
		{status: 529, body: `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`},
		{status: 200, sse: true, body: sse(
			messageStart,
			`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hold."}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		)},
	}}
	client := newTestClient(t, api, "claude-opus-5-5", "claude-sonnet-5-5")

	answer, err := client.ChatStream(context.Background(), []port.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if answer != "Hold." {
		t.Fatalf("answer = %q", answer)
	}
	if got := api.requests[1]["model"]; got != "claude-sonnet-5-5" {
		t.Fatalf("second attempt model = %v", got)
	}
}

func TestBadRequestDoesNotFailOver(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{responses: []fakeResponse{{status: 400, body: `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`}}}
	client := newTestClient(t, api, "claude-opus-5-5", "claude-sonnet-5-5")

	if _, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected an error")
	}
	if len(api.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(api.requests))
	}
}

func TestWithdrawnToolsStayDeclaredWithCallsForbidden(t *testing.T) {
	t.Parallel()

	api := &fakeAPI{responses: []fakeResponse{{status: 200, body: `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"Done."}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":1}}`}}}
	client := newTestClient(t, api, "claude-opus-5-5")

	tools := []port.Tool{{Name: "recall_memories", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}}
	ctx := port.WithToolsWithdrawn(context.Background())
	if _, err := client.ChatTools(ctx, []port.Message{{Role: "user", Content: "hi"}}, tools); err != nil {
		t.Fatalf("ChatTools: %v", err)
	}

	req := api.requests[0]
	// Dropping the tools would change the tool set earlier thinking blocks were
	// bound to; the round keeps them and forbids calls instead.
	if tools, _ := req["tools"].([]any); len(tools) != 1 {
		t.Fatalf("tools = %v", req["tools"])
	}
	if choice, _ := req["tool_choice"].(map[string]any); choice["type"] != "none" {
		t.Fatalf("tool_choice = %v", req["tool_choice"])
	}
}
