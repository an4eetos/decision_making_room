package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func sseBody(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: ")
		b.WriteString(c)
		b.WriteString("\r\n\r\n")
	}
	return b.String()
}

func TestReadStreamJoinsTextAndDeliversDeltas(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hold "}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"the line."}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"","thoughtSignature":"sig"}]}}]}`,
	)

	var deltas []string
	turn, err := readStream(strings.NewReader(body), func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if turn.Content != "Hold the line." {
		t.Fatalf("content = %q", turn.Content)
	}
	if strings.Join(deltas, "|") != "Hold |the line." {
		t.Fatalf("deltas = %q", deltas)
	}
	// One text part, carrying the trailing signature, so a replay of this turn
	// to the model is a single coherent part rather than three fragments.
	if len(turn.Parts) != 1 || turn.Parts[0].ThoughtSignature != "sig" {
		t.Fatalf("parts = %#v", turn.Parts)
	}
}

func TestReadStreamCollectsFunctionCalls(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"recall_memories","args":{"query":"visa"}},"thoughtSignature":"s1"}]}}]}`,
	)

	turn, err := readStream(strings.NewReader(body), nil)
	if err != nil {
		t.Fatalf("readStream: %v", err)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Arguments["query"] != "visa" {
		t.Fatalf("tool calls = %#v", turn.ToolCalls)
	}
	if turn.Parts[0].ThoughtSignature != "s1" {
		t.Fatalf("signature lost: %#v", turn.Parts)
	}
}

func TestReadStreamSurfacesInStreamError(t *testing.T) {
	t.Parallel()

	_, err := readStream(strings.NewReader(sseBody(`{"error":{"message":"boom"}}`)), nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatStreamFailsOverBeforeFirstByte(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if !strings.Contains(r.URL.Path, "streamGenerateContent") || r.URL.Query().Get("alt") != "sse" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if strings.Contains(r.URL.Path, "primary") {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"You exceeded your current quota","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"60s"}]}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sseBody(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"primary", "fallback"}, "embed")
	var got strings.Builder
	answer, err := client.ChatStream(context.Background(), nil, func(s string) { got.WriteString(s) })
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if answer != "ok" || got.String() != "ok" {
		t.Fatalf("answer = %q, streamed = %q", answer, got.String())
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2", hits.Load())
	}
}
