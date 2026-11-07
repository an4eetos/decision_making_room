package aiswitch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

func baseConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.Build(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// fakeOllama answers chat and embeddings, counting chat calls by model.
func fakeOllama(t *testing.T, chats *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/api/chat"):
			chats.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": map[string]any{"role": "assistant", "content": "OK"}, "done": true,
			})
		case strings.Contains(r.URL.Path, "embed"):
			_ = json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1, 0.2}}, "embedding": []float32{0.1, 0.2}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSwitchFollowsTheConfiguration(t *testing.T) {
	t.Parallel()
	var chats atomic.Int32
	srv := fakeOllama(t, &chats)

	cfg := baseConfig(t) // gemini, no key
	sw := New(func() config.Config { return cfg })

	if _, err := sw.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}}); err == nil ||
		!strings.Contains(err.Error(), "Settings") {
		t.Fatalf("missing key should point at Settings: %v", err)
	}
	if _, err := sw.Embed(context.Background(), "x"); err == nil {
		t.Fatal("embedding without a key should fail before any call")
	}
	// The model is named without a key, so vectors can be labelled.
	if id := sw.ModelID(); !strings.HasPrefix(id, "gemini:") {
		t.Fatalf("ModelID %q", id)
	}

	// Switched in Settings: the very next call goes to the new provider.
	cfg.LLMProvider, cfg.EmbedProvider, cfg.OllamaBaseURL = "ollama", "ollama", srv.URL
	reply, err := sw.Chat(context.Background(), []port.Message{{Role: "user", Content: "hi"}})
	if err != nil || reply != "OK" || chats.Load() != 1 {
		t.Fatalf("after switch: %q %v (calls %d)", reply, err, chats.Load())
	}
	if id := sw.ModelID(); !strings.HasPrefix(id, "ollama:") {
		t.Fatalf("ModelID after switch %q", id)
	}
}

func TestSwitchReusesClientsUntilTheirSettingsChange(t *testing.T) {
	t.Parallel()
	cfg := baseConfig(t)
	cfg.GeminiAPIKey = "k1"
	sw := New(func() config.Config { return cfg })

	first, _ := sw.chatClient(cfg)
	again, _ := sw.chatClient(cfg)
	if first != again {
		t.Fatal("same settings rebuilt the client, losing its failover state")
	}
	// Chat and embeddings on one Gemini configuration share the client.
	if sw.embedder(cfg) != port.Embedder(first.(port.Embedder)) {
		t.Fatal("chat and embeddings should share one Gemini client")
	}

	cfg.GeminiAPIKey = "k2"
	rotated, _ := sw.chatClient(cfg)
	if rotated == first {
		t.Fatal("a new key kept the old client")
	}
}

func TestProbeReportsBothHalves(t *testing.T) {
	t.Parallel()
	var chats atomic.Int32
	srv := fakeOllama(t, &chats)

	cfg := baseConfig(t)
	cfg.LLMProvider, cfg.OllamaBaseURL = "ollama", srv.URL // chat via Ollama, embeddings still Gemini without a key

	chat, embed := Probe(context.Background(), cfg)
	if !chat.OK || chat.Provider != "ollama" {
		t.Fatalf("chat %+v", chat)
	}
	if embed.OK || !strings.Contains(embed.Error, "Gemini") {
		t.Fatalf("embed %+v", embed)
	}
}
