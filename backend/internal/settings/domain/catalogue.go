// Package domain holds the settings a client may change, and how secrets are
// sealed.
package domain

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind tells a client which control to draw.
type Kind string

const (
	KindSecret   Kind = "secret"
	KindText     Kind = "text"
	KindChoice   Kind = "choice"
	KindBool     Kind = "bool"
	KindInt      Kind = "int"
	KindSlots    Kind = "slots"
	KindTimezone Kind = "timezone"
)

// Definition is one setting a client may change. The key is the environment
// variable's name, so the two can never drift apart.
type Definition struct {
	Key     string
	Group   string
	Label   string
	Help    string
	Kind    Kind
	Choices []string
	// Restart marks a setting the server reads once at boot.
	Restart bool
}

// maxValueRunes matches the column.
const maxValueRunes = 4096

var tiers = []string{"quick", "standard", "deep"}

// Catalogue is every setting a client may change, in display order. Anything
// absent, DATABASE_URL, HTTP_ADDR, SETTINGS_KEY and the overlay directories
// among them, comes from the environment only.
var Catalogue = []Definition{
	{Key: "LLM_PROVIDER", Group: "provider", Label: "Who answers", Kind: KindChoice,
		Choices: []string{"gemini", "anthropic", "ollama"},
		Help:    "ollama only works where the server can reach an Ollama install, which a hosted room cannot."},
	{Key: "EMBED_PROVIDER", Group: "provider", Label: "Who embeds the journal", Kind: KindChoice,
		Choices: []string{"gemini", "ollama"},
		Help:    "Unset, it follows who answers, and Gemini when Claude answers. Switching means a reindex."},

	{Key: "GEMINI_API_KEY", Group: "gemini", Label: "Gemini API key", Kind: KindSecret,
		Help: "Free at aistudio.google.com/apikey."},
	{Key: "GEMINI_CHAT_MODEL", Group: "gemini", Label: "Chat model", Kind: KindText},
	{Key: "GEMINI_FALLBACK_MODEL", Group: "gemini", Label: "Fallback model", Kind: KindText,
		Help: "Tried when the chat model is overloaded or out of quota."},
	{Key: "GEMINI_EMBED_MODEL", Group: "gemini", Label: "Embedding model", Kind: KindText,
		Help: "Changing it means a reindex: memories embedded with another model drop out of search until then."},

	{Key: "ANTHROPIC_API_KEY", Group: "anthropic", Label: "Anthropic API key", Kind: KindSecret,
		Help: "From platform.claude.com/settings/keys."},
	{Key: "ANTHROPIC_CHAT_MODEL", Group: "anthropic", Label: "Chat model", Kind: KindText},
	{Key: "ANTHROPIC_FALLBACK_MODEL", Group: "anthropic", Label: "Fallback model", Kind: KindText,
		Help: "Tried when the chat model stays overloaded. Empty disables it."},
	{Key: "ANTHROPIC_EFFORT", Group: "anthropic", Label: "Effort", Kind: KindChoice,
		Choices: []string{"", "low", "medium", "high", "xhigh", "max"},
		Help:    "Higher thinks longer and costs more. Empty sends none, for models that do not take it."},

	{Key: "OLLAMA_CHAT_MODEL", Group: "ollama", Label: "Chat model", Kind: KindText},

	{Key: "DEFAULT_TIER", Group: "answers", Label: "Default depth", Kind: KindChoice, Choices: tiers},
	{Key: "MAX_TIER", Group: "answers", Label: "Deepest allowed", Kind: KindChoice, Choices: tiers,
		Help: "A request cannot go deeper. quick turns tool calling off entirely."},

	{Key: "CHECKIN_ENABLED", Group: "checkins", Label: "Check-ins", Kind: KindBool},
	{Key: "CHECKIN_SLOTS", Group: "checkins", Label: "Slots", Kind: KindSlots,
		Help: "name@HH:MM pairs, comma-separated, e.g. morning@08:00,evening@21:00."},
	{Key: "CHECKIN_TZ", Group: "checkins", Label: "Timezone", Kind: KindTimezone, Restart: true,
		Help: "An IANA name such as Asia/Almaty. Check-ins and the campaign's days use it; it applies after a restart."},

	{Key: "SORTIE_MINUTES", Group: "campaign", Label: "Sortie length (minutes)", Kind: KindInt,
		Help: "The default focus block, 5 to 480."},
	{Key: "COMMITMENT_EXTRACTION_ENABLED", Group: "campaign", Label: "Propose open loops from chat", Kind: KindBool},
	{Key: "CAMPAIGN_EXTRACTION_ENABLED", Group: "campaign", Label: "Propose map items from chat", Kind: KindBool},
	{Key: "CAMPAIGN_STAFF_ENABLED", Group: "campaign", Label: "Read the map into every answer", Kind: KindBool},
}

var byKey = func() map[string]Definition {
	m := make(map[string]Definition, len(Catalogue))
	for _, d := range Catalogue {
		m[d.Key] = d
	}
	return m
}()

// Lookup finds a key in the catalogue.
func Lookup(key string) (Definition, bool) {
	d, ok := byKey[key]
	return d, ok
}

// Exposed reports whether a client may set key.
func Exposed(key string) bool {
	_, ok := byKey[key]
	return ok
}

var ErrInvalidValue = errors.New("invalid value")

// Normalize checks a value's shape and returns it as it is stored. Whether
// the value makes sense with the rest of the configuration is decided by
// building the configuration with it.
func (d Definition) Normalize(value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxValueRunes {
		return "", fmt.Errorf("%w: %s is longer than %d characters", ErrInvalidValue, d.Key, maxValueRunes)
	}
	switch d.Kind {
	case KindSecret:
		if value == "" {
			return "", fmt.Errorf("%w: %s is empty; send null to remove it", ErrInvalidValue, d.Key)
		}
		if strings.ContainsAny(value, " \t\r\n") {
			return "", fmt.Errorf("%w: %s contains whitespace", ErrInvalidValue, d.Key)
		}
	case KindChoice:
		if !slices.Contains(d.Choices, value) {
			return "", fmt.Errorf("%w: %s must be one of %s", ErrInvalidValue, d.Key, strings.Join(d.Choices, ", "))
		}
	case KindBool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return "", fmt.Errorf("%w: %s must be true or false", ErrInvalidValue, d.Key)
		}
		value = strconv.FormatBool(b)
	case KindInt:
		if _, err := strconv.Atoi(value); err != nil {
			return "", fmt.Errorf("%w: %s must be a whole number", ErrInvalidValue, d.Key)
		}
	case KindTimezone:
		if value == "" {
			break
		}
		if _, err := time.LoadLocation(value); err != nil {
			return "", fmt.Errorf("%w: %s: unknown timezone %q", ErrInvalidValue, d.Key, value)
		}
	case KindText, KindSlots:
	default:
		return "", fmt.Errorf("%w: %s has unknown kind %q", ErrInvalidValue, d.Key, d.Kind)
	}
	return value, nil
}
