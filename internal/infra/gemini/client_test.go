package gemini

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// Overload is reported as a 503 with a "high demand" message, not a 429. Chat
// previously did not retry at all, so a spike of a few seconds failed the whole
// question.
func TestChatRetriesTransientOverload(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"This model is currently experiencing high demand."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"recovered"}]}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"test-model"}, "embed")
	answer, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if answer != "recovered" {
		t.Fatalf("answer = %q", answer)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("expected one retry, got %d calls", got)
	}
}

// A permanent error must not be retried — a retired model will never come back,
// and retrying only delays a clear message.
func TestChatDoesNotRetryPermanentErrors(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"This model is no longer available."}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"test-model"}, "embed")
	if _, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected no retry, got %d calls", got)
	}
}

// A retry delay longer than a person will wait is not worth taking.
func TestPostGivesUpOnLongRetryDelays(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":429,"message":"quota","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"120s"}]}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"test-model"}, "embed")

	done := make(chan error, 1)
	go func() {
		_, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client waited on a 120s retry delay instead of giving up")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected a single attempt, got %d", got)
	}
}

// Quota exhaustion is the most likely reason to need a second model, and it was
// the one case failover did not fire: the message contains none of the markers
// the old string matching looked for.
func TestFailsOverToNextModelOnQuotaExhaustion(t *testing.T) {
	t.Parallel()

	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path)
		if strings.Contains(r.URL.Path, "primary") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing details."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"from the fallback"}]}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"primary", "fallback"}, "embed")
	answer, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if answer != "from the fallback" {
		t.Fatalf("answer = %q; expected the fallback model to be used", answer)
	}
	if len(seen) < 2 {
		t.Fatalf("expected the fallback model to be tried, calls: %v", seen)
	}
}

// A retired model will never come back, so trying the fallback is right but
// retrying the same one is not.
func TestDoesNotFailOverOnPermanentModelError(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":400,"message":"API key not valid"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"primary", "fallback"}, "embed")
	if _, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("a bad API key should not be retried or failed over; got %d calls", got)
	}
}

// The status must survive into the error, since every retry decision keys off it.
func TestAPIErrorCarriesStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"only"}, "embed")
	_, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("expected an error")
	}

	var apiErr *apiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an *apiError, got %T", err)
	}
	if apiErr.status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", apiErr.status)
	}
	if !apiErr.Retryable() {
		t.Fatal("503 should be retryable")
	}
}
