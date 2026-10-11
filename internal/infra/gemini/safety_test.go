package gemini

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

	"github.com/an4eetos/decision-room/internal/memory/port"
)

func TestRelaxedSafetyIsSentOnlyWhenAsked(t *testing.T) {
	t.Parallel()

	var (
		mu     sync.Mutex
		bodies []generateRequest
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req generateRequest
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		bodies = append(bodies, req)
		mu.Unlock()
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Move."}]}}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"test-model"}, "embed")
	messages := []port.Message{{Role: "user", Content: "go"}}
	if _, err := client.Chat(context.Background(), messages); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Chat(port.WithRelaxedSafety(context.Background()), messages); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies[0].SafetySettings) != 0 {
		t.Errorf("an ordinary call must keep Google's defaults, sent %+v", bodies[0].SafetySettings)
	}
	relaxed := bodies[1].SafetySettings
	if len(relaxed) != 1 || relaxed[0].Category != "HARM_CATEGORY_HARASSMENT" || relaxed[0].Threshold != "BLOCK_ONLY_HIGH" {
		t.Errorf("relaxed call sent %+v; want only harassment loosened", relaxed)
	}
	for _, s := range relaxed {
		if s.Category == "HARM_CATEGORY_HATE_SPEECH" {
			t.Error("hate speech must never be loosened")
		}
	}
}

func TestBlockedReplySaysSo(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"finishReason":"SAFETY","content":{"role":"model","parts":[]},
			"safetyRatings":[{"category":"HARM_CATEGORY_HARASSMENT","probability":"HIGH","blocked":true},
			                 {"category":"HARM_CATEGORY_HATE_SPEECH","probability":"NEGLIGIBLE"}]}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"test-model"}, "embed")
	_, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "go"}})
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "safety: harassment") {
		t.Fatalf("got %v, want a blocked error naming safety and the category", err)
	}
	if strings.Contains(err.Error(), "hate_speech") {
		t.Fatal("only the category that blocked belongs in the error")
	}
}

func TestBlockedPromptSaysSo(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT"}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "key", []string{"test-model"}, "embed")
	_, err := client.Chat(context.Background(), []port.Message{{Role: "user", Content: "go"}})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v, want a blocked error", err)
	}
}

// Half a reply stored as if it were the whole one is worse than saying it was
// stopped.
func TestStreamCutOffByAFilterIsAnError(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Listen, you "}]}}]}`,
		`{"candidates":[{"finishReason":"SAFETY","content":{"role":"model","parts":[]}}]}`,
	)
	_, err := readStream(strings.NewReader(body), nil)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v, want a blocked error", err)
	}
}

func TestStreamThatEndsNormallyIsNotBlocked(t *testing.T) {
	t.Parallel()

	body := sseBody(
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Move."}]}}]}`,
		`{"candidates":[{"finishReason":"STOP","content":{"role":"model","parts":[]}}]}`,
	)
	turn, err := readStream(strings.NewReader(body), nil)
	if err != nil || turn.Content != "Move." {
		t.Fatalf("got %q, %v", turn.Content, err)
	}
}
