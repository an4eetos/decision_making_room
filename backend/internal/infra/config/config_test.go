package config

import (
	"strings"
	"testing"
)

func TestAnthropicEmbedsWithGeminiByDefault(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("GEMINI_API_KEY", "g")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EmbedProvider != "gemini" {
		t.Fatalf("EmbedProvider = %q", cfg.EmbedProvider)
	}
	if strings.Join(cfg.AnthropicChatModels, ",") != "claude-opus-5-5,claude-sonnet-5-5" {
		t.Fatalf("AnthropicChatModels = %v", cfg.AnthropicChatModels)
	}
}

// A missing key no longer stops the room from starting, since it can be typed
// into Settings; it is reported on the call that needs it.
func TestAnthropicWithoutAnEmbedderIsReportedNotFatal(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("GEMINI_API_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.ChatProblem(); err != nil {
		t.Fatalf("ChatProblem: %v", err)
	}
	if err := cfg.EmbedProblem(); err == nil || !strings.Contains(err.Error(), "EMBED_PROVIDER=ollama") {
		t.Fatalf("EmbedProblem = %v", err)
	}
}

func TestBuildReadsAnyLookup(t *testing.T) {
	t.Parallel()
	vals := map[string]string{"LLM_PROVIDER": "anthropic", "ANTHROPIC_EFFORT": "", "SORTIE_MINUTES": "50"}
	cfg, err := Build(func(k string) (string, bool) { v, ok := vals[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LLMProvider != "anthropic" || cfg.SortieMinutes != 50 {
		t.Fatalf("cfg %+v", cfg)
	}
	// Set but empty disables effort rather than falling back to the default.
	if cfg.AnthropicEffort != "" {
		t.Fatalf("AnthropicEffort = %q", cfg.AnthropicEffort)
	}
	if cfg.ChatProblem() == nil {
		t.Fatal("anthropic without a key should report a problem")
	}

	vals["LLM_PROVIDER"] = "openai"
	if _, err := Build(func(k string) (string, bool) { v, ok := vals[k]; return v, ok }); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestAnthropicWithLocalEmbeddings(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("EMBED_PROVIDER", "ollama")
	t.Setenv("ANTHROPIC_FALLBACK_MODEL", "")
	t.Setenv("ANTHROPIC_EFFORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Set but empty disables, rather than falling back to the default.
	if len(cfg.AnthropicChatModels) != 1 || cfg.AnthropicEffort != "" {
		t.Fatalf("models = %v, effort = %q", cfg.AnthropicChatModels, cfg.AnthropicEffort)
	}
}

func TestEmbedProviderFollowsChatProvider(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "ollama")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EmbedProvider != "ollama" {
		t.Fatalf("EmbedProvider = %q", cfg.EmbedProvider)
	}
}
