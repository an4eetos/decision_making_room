package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatToolsStreamReadsNDJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !req.Stream {
			t.Errorf("stream not requested: %v %#v", err, req)
		}
		_, _ = w.Write([]byte(strings.Join([]string{
			`{"message":{"role":"assistant","content":"Hold "},"done":false}`,
			`{"message":{"role":"assistant","content":"the line."},"done":false}`,
			`{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"recall_memories","arguments":{"query":"visa"}}}]},"done":false}`,
			`{"message":{"role":"assistant","content":""},"done":true}`,
		}, "\n") + "\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "chat", "embed")
	var deltas []string
	turn, err := client.ChatToolsStream(context.Background(), nil, nil, func(s string) { deltas = append(deltas, s) })
	if err != nil {
		t.Fatalf("ChatToolsStream: %v", err)
	}
	if turn.Content != "Hold the line." || strings.Join(deltas, "") != "Hold the line." {
		t.Fatalf("content = %q, deltas = %q", turn.Content, deltas)
	}
	if len(turn.ToolCalls) != 1 || turn.ToolCalls[0].Arguments["query"] != "visa" {
		t.Fatalf("tool calls = %#v", turn.ToolCalls)
	}
}

func TestChatStreamSurfacesError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"error":"model not found"}` + "\n"))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "chat", "embed").ChatStream(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "model not found") {
		t.Fatalf("err = %v", err)
	}
}
