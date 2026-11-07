package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

// collect records what the client says about its waits.
func collect() (context.Context, func() []string) {
	var mu sync.Mutex
	var notes []string
	ctx := port.WithNotice(context.Background(), func(s string) {
		mu.Lock()
		defer mu.Unlock()
		notes = append(notes, s)
	})
	return ctx, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), notes...)
	}
}

// A retry used to be a silent second or twenty under "thinking". It is said.
func TestRetryIsAnnounced(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"code":503,"message":"high demand"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`))
	}))
	defer server.Close()

	ctx, notes := collect()
	client := NewClient(server.URL, "key", []string{"m"}, "embed")
	if _, err := client.Chat(ctx, []port.Message{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	got := notes()
	if len(got) != 1 || !strings.Contains(got[0], "overloaded") || !strings.Contains(got[0], "1s") {
		t.Fatalf("notes = %q", got)
	}
}

func TestSwitchToFallbackIsAnnounced(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "primary") {
			// A wait too long to sit out goes straight to the next model.
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"quota","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"120s"}]}}`))
			return
		}
		_, _ = w.Write([]byte("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}]}\r\n\r\n"))
	}))
	defer server.Close()

	ctx, notes := collect()
	client := NewClient(server.URL, "key", []string{"primary", "fallback"}, "embed")
	if _, err := client.ChatStream(ctx, []port.Message{{Role: "user", Content: "hi"}}, nil); err != nil {
		t.Fatalf("stream: %v", err)
	}
	got := notes()
	if len(got) != 1 || got[0] != "primary is unavailable. Switching to fallback" {
		t.Fatalf("notes = %q", got)
	}
}
