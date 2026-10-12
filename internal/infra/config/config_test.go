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

func TestAnthropicWithoutAnEmbedderFails(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "anthropic")
	t.Setenv("ANTHROPIC_API_KEY", "a")
	t.Setenv("GEMINI_API_KEY", "")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "EMBED_PROVIDER=ollama") {
		t.Fatalf("err = %v", err)
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
