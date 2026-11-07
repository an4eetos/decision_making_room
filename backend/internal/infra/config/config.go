package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL      string
	LLMProvider      string
	OllamaBaseURL    string
	OllamaChatModel  string
	OllamaEmbedModel string
	GeminiAPIKey     string
	GeminiBaseURL    string
	GeminiChatModel  string
	GeminiChatModels []string
	// GeminiFallbackModel is appended to the chat model list when no explicit
	// failover chain is configured. Free-tier quota is per model, so a second
	// model is the difference between a failed question and a slightly weaker
	// answer. Set it empty to disable.
	GeminiFallbackModel string
	GeminiEmbedModel    string

	AnthropicAPIKey     string
	AnthropicBaseURL    string
	AnthropicChatModel  string
	AnthropicChatModels []string
	// AnthropicFallbackModel is tried when the chat model stays overloaded or
	// rate limited after the SDK's own retries. Refusals are handled server
	// side and never reach it.
	AnthropicFallbackModel string
	// AnthropicEffort is sent as output_config.effort. Empty sends nothing,
	// for models that do not take the parameter.
	AnthropicEffort string

	// EmbedProvider is "gemini" or "ollama". It defaults to LLMProvider, and to
	// gemini when that is anthropic, which has no embeddings endpoint.
	EmbedProvider string

	HTTPAddr string
	// RoomPassword, when set, is the room's password: it creates the owner on
	// first boot and replaces a different stored one, signing every device
	// out. Unset, the first password is chosen on /setup.
	RoomPassword string
	// SettingsKey seals the secrets typed into a client. Environment only:
	// it never goes in the database it protects.
	SettingsKey string
	// PasswordLogin turns the password off when false, leaving only OIDC.
	PasswordLogin bool
	// PublicURL is where browsers reach the room; the OIDC callback is
	// registered under it. Defaults to Railway's public domain, then to
	// localhost on HTTP_ADDR's port.
	PublicURL string

	// OIDC sign-in, on when OIDCIssuer is set: any OpenID Connect provider
	// (Google, Microsoft, Auth0, Authentik, Keycloak). OIDCAllowedEmails is
	// required with it: the room has one owner, and without the list any
	// account at the provider would get in.
	OIDCIssuer        string
	OIDCClientID      string
	OIDCClientSecret  string
	OIDCAllowedEmails []string
	// OIDCName labels the sign-in button.
	OIDCName string
	// OIDCAppRedirects are the app callback URLs a sign-in may return to with
	// its one-time code. Anything else is refused, so a link cannot send a
	// code to someone else's app.
	OIDCAppRedirects []string

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
	// GeneralsDir optionally overlays the shipped roster. It holds generals/
	// and styles/ subdirectories of markdown with YAML frontmatter.
	GeneralsDir string
	// ModesDir optionally overlays the shipped conversation modes.
	ModesDir string

	// CommitmentExtraction proposes commitments from what you write in chat.
	// Each qualifying turn costs one model call, gated by a free prefilter.
	CommitmentExtraction bool
	// CommitmentStaleAfter is how long an open commitment can go untouched
	// before it is marked stale and drops out of check-ins.
	CommitmentStaleAfter time.Duration

	// CampaignExtraction proposes objectives, obstacles and unknowns from what
	// you write. Like open loops, a qualifying turn costs one background model
	// call behind a free prefilter; interrogation turns cost none.
	CampaignExtraction bool
	// CampaignStaff reads the campaign map into every answer, so the generals
	// appraise the map and not just the question. One database read per
	// answer, no model call; off, the room answers as it did before the map.
	CampaignStaff bool
	// SortieMinutes is the length of a default sortie, the focus block a
	// front's hours are logged in, and what an order is estimated to cost
	// before a front has a rate of its own.
	SortieMinutes int

	// Check-ins are off by default: a tool that starts messaging you unasked
	// is one people uninstall. Slots are "name@HH:MM" pairs.
	CheckinEnabled   bool
	CheckinSlots     string
	CheckinTimezone  string
	CheckinGrace     time.Duration
	CheckinIdleAfter time.Duration
	// CheckinNotifyCmd runs on each check-in, with {title} and {body}
	// substituted. Executed directly, never through a shell.
	CheckinNotifyCmd    string
	JournalWatchDir     string
	JournalWatchEnabled bool
}

// Lookup returns a variable's value and whether it is set at all.
type Lookup func(key string) (string, bool)

// Load reads the process environment, with .env filling what it leaves unset.
func Load() (Config, error) {
	if err := loadDotEnv(".env"); err != nil {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}
	return Build(os.LookupEnv)
}

// Build parses a Config from any source of variables. The environment is one;
// the Resolver layers stored settings under it and builds again, so a value
// typed in a client is parsed, defaulted and validated exactly like one from
// the environment.
func Build(get Lookup) (Config, error) {
	e := vars{get: get}

	sortieMinutes, err := strconv.Atoi(e.or("SORTIE_MINUTES", "25"))
	if err != nil || sortieMinutes < 5 || sortieMinutes > 480 {
		return Config{}, fmt.Errorf("SORTIE_MINUTES must be 5 to 480 minutes")
	}

	topK, err := strconv.Atoi(e.or("RETRIEVAL_TOP_K", "8"))
	if err != nil {
		return Config{}, fmt.Errorf("parse RETRIEVAL_TOP_K: %w", err)
	}

	candidates, err := strconv.Atoi(e.or("RETRIEVAL_CANDIDATES", "30"))
	if err != nil {
		return Config{}, fmt.Errorf("parse RETRIEVAL_CANDIDATES: %w", err)
	}

	journalDir := e.or("JOURNAL_WATCH_DIR", "./journal")
	aboutMeFile := e.or("ABOUT_ME_FILE", filepath.Join(journalDir, "about-me.md"))
	contextDir := e.or("CONTEXT_DIR", filepath.Join(journalDir, "context"))

	maxRunes, err := strconv.Atoi(e.or("INITIAL_CONTEXT_MAX_RUNES", "0"))
	if err != nil {
		return Config{}, fmt.Errorf("parse INITIAL_CONTEXT_MAX_RUNES: %w", err)
	}

	cfg := Config{
		DatabaseURL:         e.or("DATABASE_URL", "postgres://room:room@localhost:5432/decision_room?sslmode=disable"),
		LLMProvider:         e.or("LLM_PROVIDER", "gemini"),
		OllamaBaseURL:       e.or("OLLAMA_BASE_URL", "http://localhost:11434"),
		OllamaChatModel:     e.or("OLLAMA_CHAT_MODEL", "qwen3:8b"),
		OllamaEmbedModel:    e.or("OLLAMA_EMBED_MODEL", "nomic-embed-text"),
		GeminiAPIKey:        e.str("GEMINI_API_KEY"),
		GeminiBaseURL:       e.or("GEMINI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta"),
		GeminiChatModel:     e.or("GEMINI_CHAT_MODEL", "gemini-flash-latest"),
		GeminiChatModels:    e.csv("GEMINI_CHAT_MODELS"),
		GeminiFallbackModel: e.or("GEMINI_FALLBACK_MODEL", "gemini-flash-lite-latest"),
		GeminiEmbedModel:    e.or("GEMINI_EMBED_MODEL", "gemini-embedding-001"),
		AnthropicAPIKey:     e.str("ANTHROPIC_API_KEY"),
		AnthropicBaseURL:    e.str("ANTHROPIC_BASE_URL"),
		AnthropicChatModel:  e.or("ANTHROPIC_CHAT_MODEL", "claude-opus-5-5"),
		AnthropicChatModels: e.csv("ANTHROPIC_CHAT_MODELS"),
		// Set but empty means "disabled", so these two read the variable
		// directly instead of treating empty as unset.
		AnthropicFallbackModel: e.orAllowEmpty("ANTHROPIC_FALLBACK_MODEL", "claude-sonnet-5-5"),
		AnthropicEffort:        e.orAllowEmpty("ANTHROPIC_EFFORT", "medium"),
		EmbedProvider:          e.str("EMBED_PROVIDER"),
		HTTPAddr:               e.or("HTTP_ADDR", listenAddrFromPort(e)),
		RoomPassword:           e.str("ROOM_PASSWORD"),
		SettingsKey:            e.str("SETTINGS_KEY"),
		PasswordLogin:          e.bool("PASSWORD_LOGIN_ENABLED", true),
		OIDCIssuer:             strings.TrimRight(strings.TrimSpace(e.str("OIDC_ISSUER")), "/"),
		OIDCClientID:           strings.TrimSpace(e.str("OIDC_CLIENT_ID")),
		OIDCClientSecret:       e.str("OIDC_CLIENT_SECRET"),
		OIDCAllowedEmails:      e.csv("OIDC_ALLOWED_EMAILS"),
		OIDCName:               e.str("OIDC_NAME"),
		OIDCAppRedirects:       e.csv("OIDC_APP_REDIRECTS"),
		RetrievalTopK:          topK,
		RetrievalCandidates:    candidates,
		DefaultTier:            e.or("DEFAULT_TIER", "standard"),
		MaxTier:                e.or("MAX_TIER", "deep"),
		AboutMeFile:            aboutMeFile,
		ContextDir:             contextDir,
		InitialContextMaxRunes: maxRunes,
		WebRoot:                e.or("WEB_ROOT", "../client/web"), // the web client, outside the Go module
		RelocationCatalogDir:   e.str("RELOCATION_CATALOG_DIR"),
		GeneralsDir:            e.str("GENERALS_DIR"),
		ModesDir:               e.str("MODES_DIR"),
		CommitmentExtraction:   e.bool("COMMITMENT_EXTRACTION_ENABLED", true),
		CampaignExtraction:     e.bool("CAMPAIGN_EXTRACTION_ENABLED", true),
		CampaignStaff:          e.bool("CAMPAIGN_STAFF_ENABLED", true),
		SortieMinutes:          sortieMinutes,
		CheckinEnabled:         e.bool("CHECKIN_ENABLED", false),
		CheckinSlots:           e.or("CHECKIN_SLOTS", "morning@08:00,midday@13:00,evening@21:00"),
		CheckinTimezone:        e.str("CHECKIN_TZ"),
		CheckinNotifyCmd:       e.str("CHECKIN_NOTIFY_CMD"),
		JournalWatchDir:        journalDir,
		JournalWatchEnabled:    e.bool("JOURNAL_WATCH_ENABLED", journalDir != ""),
	}

	staleAfter, err := time.ParseDuration(e.or("COMMITMENT_STALE_AFTER", "336h"))
	if err != nil {
		return Config{}, fmt.Errorf("parse COMMITMENT_STALE_AFTER: %w", err)
	}
	cfg.CommitmentStaleAfter = staleAfter

	if cfg.CheckinGrace, err = time.ParseDuration(e.or("CHECKIN_GRACE", "90m")); err != nil {
		return Config{}, fmt.Errorf("parse CHECKIN_GRACE: %w", err)
	}
	if cfg.CheckinIdleAfter, err = time.ParseDuration(e.or("CHECKIN_IDLE_AFTER", "6h")); err != nil {
		return Config{}, fmt.Errorf("parse CHECKIN_IDLE_AFTER: %w", err)
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	if err := resolveSignIn(&cfg, e); err != nil {
		return Config{}, err
	}

	switch cfg.LLMProvider {
	case "ollama", "gemini", "anthropic":
	default:
		return Config{}, fmt.Errorf("unsupported LLM_PROVIDER %q (use gemini, anthropic or ollama)", cfg.LLMProvider)
	}

	if cfg.EmbedProvider == "" {
		cfg.EmbedProvider = cfg.LLMProvider
		if cfg.LLMProvider == "anthropic" {
			cfg.EmbedProvider = "gemini"
		}
	}
	switch cfg.EmbedProvider {
	case "ollama", "gemini":
	default:
		return Config{}, fmt.Errorf("unsupported EMBED_PROVIDER %q (use gemini or ollama)", cfg.EmbedProvider)
	}

	if len(cfg.AnthropicChatModels) == 0 {
		cfg.AnthropicChatModels = []string{cfg.AnthropicChatModel}
		if fb := cfg.AnthropicFallbackModel; fb != "" && fb != cfg.AnthropicChatModel {
			cfg.AnthropicChatModels = append(cfg.AnthropicChatModels, fb)
		}
	}
	if len(cfg.GeminiChatModels) == 0 {
		cfg.GeminiChatModels = []string{cfg.GeminiChatModel}
		if fb := cfg.GeminiFallbackModel; fb != "" && fb != cfg.GeminiChatModel {
			cfg.GeminiChatModels = append(cfg.GeminiChatModels, fb)
		}
	}

	return cfg, nil
}

// vars reads variables through a Lookup. Empty counts as unset, except where
// orAllowEmpty says set-but-empty means "disabled".
type vars struct {
	get Lookup
}

func (e vars) str(key string) string {
	v, _ := e.get(key)
	return v
}

func (e vars) or(key, fallback string) string {
	if v := e.str(key); v != "" {
		return v
	}
	return fallback
}

func (e vars) orAllowEmpty(key, fallback string) string {
	if v, ok := e.get(key); ok {
		return strings.TrimSpace(v)
	}
	return fallback
}

func (e vars) bool(key string, fallback bool) bool {
	v, ok := e.get(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func (e vars) csv(key string) []string {
	raw := strings.TrimSpace(e.str(key))
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

// ChatProblem says why the configured chat provider cannot answer, or nil.
// Keys can be typed into a client now, so a missing one is reported on the
// question that needs it, not at boot.
func (c Config) ChatProblem() error {
	switch {
	case c.LLMProvider == "gemini" && c.GeminiAPIKey == "":
		return fmt.Errorf("no Gemini API key: add one in Settings, or set GEMINI_API_KEY")
	case c.LLMProvider == "anthropic" && c.AnthropicAPIKey == "":
		return fmt.Errorf("no Anthropic API key: add one in Settings, or set ANTHROPIC_API_KEY")
	}
	return nil
}

// EmbedProblem says why the embedder cannot run, or nil.
func (c Config) EmbedProblem() error {
	if c.EmbedProvider == "gemini" && c.GeminiAPIKey == "" {
		// Reached with LLM_PROVIDER=anthropic too: Claude answers, but
		// something still has to turn the journal into vectors.
		return fmt.Errorf("no Gemini API key for embeddings: add one in Settings, set GEMINI_API_KEY, or set EMBED_PROVIDER=ollama to embed locally (Anthropic has no embeddings API)")
	}
	return nil
}

// defaultAppRedirect is the Flutter app's callback scheme.
const defaultAppRedirect = "decisionroom://auth"

// resolveSignIn fills the sign-in defaults and refuses a configuration that
// would lock the owner out or let a stranger in.
func resolveSignIn(cfg *Config, e vars) error {
	cfg.PublicURL = strings.TrimRight(strings.TrimSpace(e.str("PUBLIC_URL")), "/")
	if cfg.PublicURL == "" {
		if domain := strings.TrimSpace(e.str("RAILWAY_PUBLIC_DOMAIN")); domain != "" {
			cfg.PublicURL = "https://" + domain
		} else {
			cfg.PublicURL = "http://localhost" + portSuffix(cfg.HTTPAddr)
		}
	}

	if cfg.OIDCIssuer == "" {
		if !cfg.PasswordLogin {
			return fmt.Errorf("PASSWORD_LOGIN_ENABLED=false needs OIDC_ISSUER: with neither, nobody can sign in")
		}
		return nil
	}

	if !strings.HasPrefix(cfg.OIDCIssuer, "https://") {
		return fmt.Errorf("OIDC_ISSUER must be an https URL")
	}
	if cfg.OIDCClientID == "" {
		return fmt.Errorf("OIDC_CLIENT_ID is required with OIDC_ISSUER")
	}
	if len(cfg.OIDCAllowedEmails) == 0 {
		return fmt.Errorf("OIDC_ALLOWED_EMAILS is required with OIDC_ISSUER: without it any account at %s could sign in", cfg.OIDCIssuer)
	}
	for i, e := range cfg.OIDCAllowedEmails {
		cfg.OIDCAllowedEmails[i] = strings.ToLower(e)
	}
	if cfg.OIDCName == "" {
		cfg.OIDCName = "single sign-on"
		if cfg.OIDCIssuer == "https://accounts.google.com" {
			cfg.OIDCName = "Google"
		}
	}
	if len(cfg.OIDCAppRedirects) == 0 {
		cfg.OIDCAppRedirects = []string{defaultAppRedirect}
	}
	return nil
}

// portSuffix turns ":8080" or "0.0.0.0:8080" into ":8080".
func portSuffix(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		if port := addr[i+1:]; port != "" && port != "80" {
			return ":" + port
		}
	}
	return ""
}

// listenAddrFromPort is the default HTTP_ADDR: ":$PORT" when a host such as
// Railway assigns the port, ":8080" otherwise. HTTP_ADDR set explicitly wins.
func listenAddrFromPort(e vars) string {
	if port := strings.TrimSpace(e.str("PORT")); port != "" {
		return ":" + port
	}
	return ":8080"
}
