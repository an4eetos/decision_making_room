package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	DatabaseURL         string
	LLMProvider         string
	OllamaBaseURL       string
	OllamaChatModel     string
	OllamaEmbedModel    string
	GeminiAPIKey        string
	GeminiBaseURL       string
	GeminiChatModel     string
	GeminiChatModels    []string
	GeminiEmbedModel    string
	HTTPAddr            string
	RetrievalTopK       int
	RetrievalCandidates int

	// DefaultTier is the answer depth used when a request does not ask for one.
	// MaxTier is the ceiling. It replaces the old AGENTIC_RAG_ENABLED boolean,
	// which silently switched code paths instead of naming a limit; setting it
	// to "quick" disables tool calling entirely.
	DefaultTier            string
	MaxTier                string
	AboutMeFile            string
	ContextDir             string
	InitialContextMaxRunes int
	WebRoot                string
	// RelocationCatalogDir optionally overlays the shipped relocation knowledge
	// base. It holds catalog/ and pitfalls/ subdirectories; an entry replaces a
	// shipped one by id, or is appended when the id is new.
	RelocationCatalogDir string
	JournalWatchDir      string
	JournalWatchEnabled  bool
}

func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	topK, err := strconv.Atoi(envOrDefault("RETRIEVAL_TOP_K", "8"))
	if err != nil {
		return Config{}, fmt.Errorf("parse RETRIEVAL_TOP_K: %w", err)
	}

	candidates, err := strconv.Atoi(envOrDefault("RETRIEVAL_CANDIDATES", "30"))
	if err != nil {
		return Config{}, fmt.Errorf("parse RETRIEVAL_CANDIDATES: %w", err)
	}

	journalDir := envOrDefault("JOURNAL_WATCH_DIR", "./journal")
	aboutMeFile := envOrDefault("ABOUT_ME_FILE", filepath.Join(journalDir, "about-me.md"))
	contextDir := envOrDefault("CONTEXT_DIR", filepath.Join(journalDir, "context"))

	maxRunes, err := strconv.Atoi(envOrDefault("INITIAL_CONTEXT_MAX_RUNES", "0"))
	if err != nil {
		return Config{}, fmt.Errorf("parse INITIAL_CONTEXT_MAX_RUNES: %w", err)
	}

	cfg := Config{
		DatabaseURL:            envOrDefault("DATABASE_URL", "postgres://room:room@localhost:5432/decision_room?sslmode=disable"),
		LLMProvider:            envOrDefault("LLM_PROVIDER", "gemini"),
		OllamaBaseURL:          envOrDefault("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaChatModel:        envOrDefault("OLLAMA_CHAT_MODEL", "qwen3:8b"),
		OllamaEmbedModel:       envOrDefault("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		GeminiAPIKey:           os.Getenv("GEMINI_API_KEY"),
		GeminiBaseURL:          envOrDefault("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		GeminiChatModel:        envOrDefault("GEMINI_CHAT_MODEL", "gemini-flash-latest"),
		GeminiChatModels:       parseCSVEnv("GEMINI_CHAT_MODELS"),
		GeminiEmbedModel:       envOrDefault("GEMINI_EMBED_MODEL", "gemini-embedding-001"),
		HTTPAddr:               envOrDefault("HTTP_ADDR", ":8080"),
		RetrievalTopK:          topK,
		RetrievalCandidates:    candidates,
		DefaultTier:            envOrDefault("DEFAULT_TIER", "standard"),
		MaxTier:                envOrDefault("MAX_TIER", "deep"),
		AboutMeFile:            aboutMeFile,
		ContextDir:             contextDir,
		InitialContextMaxRunes: maxRunes,
		WebRoot:                os.Getenv("WEB_ROOT"), // empty => assets embedded in the binary
		RelocationCatalogDir:   os.Getenv("RELOCATION_CATALOG_DIR"),
		JournalWatchDir:        journalDir,
		JournalWatchEnabled:    envBoolOrDefault("JOURNAL_WATCH_ENABLED", journalDir != ""),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	switch cfg.LLMProvider {
	case "ollama", "gemini":
	default:
		return Config{}, fmt.Errorf("unsupported LLM_PROVIDER %q (use ollama or gemini)", cfg.LLMProvider)
	}

	if cfg.LLMProvider == "gemini" && cfg.GeminiAPIKey == "" {
		return Config{}, fmt.Errorf("GEMINI_API_KEY is required when LLM_PROVIDER=gemini")
	}
	if len(cfg.GeminiChatModels) == 0 {
		cfg.GeminiChatModels = []string{cfg.GeminiChatModel}
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBoolOrDefault(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func parseCSVEnv(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
