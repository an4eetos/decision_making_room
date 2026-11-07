package domain

import (
	"errors"
	"strings"
	"testing"
)

const testKey = "0123456789abcdef0123456789abcdef-test"

func TestCipherRoundTrip(t *testing.T) {
	t.Parallel()
	c, err := NewCipher(testKey)
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := c.Seal("GEMINI_API_KEY", "AIza-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, "v1:") || strings.Contains(sealed, "secret") {
		t.Fatalf("sealed %q", sealed)
	}
	if again, _ := c.Seal("GEMINI_API_KEY", "AIza-secret-value"); again == sealed {
		t.Fatal("nonce reused")
	}

	plain, err := c.Open("GEMINI_API_KEY", sealed)
	if err != nil || plain != "AIza-secret-value" {
		t.Fatalf("Open: %q %v", plain, err)
	}

	// Moved onto another setting's row: refused.
	if _, err := c.Open("ANTHROPIC_API_KEY", sealed); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("other name: %v", err)
	}
	// Another SETTINGS_KEY: refused.
	other, _ := NewCipher(testKey + "-rotated")
	if _, err := other.Open("GEMINI_API_KEY", sealed); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("other key: %v", err)
	}
	// Tampered: refused.
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := c.Open("GEMINI_API_KEY", tampered); !errors.Is(err, ErrUndecryptable) {
		t.Fatalf("tampered: %v", err)
	}
}

func TestCipherKeyRules(t *testing.T) {
	t.Parallel()
	if _, err := NewCipher(""); !errors.Is(err, ErrNoSettingsKey) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := NewCipher("short"); !errors.Is(err, ErrShortKey) {
		t.Fatalf("short: %v", err)
	}
}

func TestMask(t *testing.T) {
	t.Parallel()
	if got := Mask("AIzaSyD-very-long-key-1234"); got != "••••1234" {
		t.Fatalf("got %q", got)
	}
	if got := Mask("short"); got != "••••" {
		t.Fatalf("short secret shows characters: %q", got)
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	def := func(key string) Definition {
		d, ok := Lookup(key)
		if !ok {
			t.Fatalf("%s not in catalogue", key)
		}
		return d
	}

	ok := map[string][2]string{
		"LLM_PROVIDER":     {"anthropic", "anthropic"},
		"CHECKIN_ENABLED":  {"1", "true"},
		"SORTIE_MINUTES":   {" 50 ", "50"},
		"CHECKIN_TZ":       {"Asia/Almaty", "Asia/Almaty"},
		"ANTHROPIC_EFFORT": {"", ""},
		"GEMINI_API_KEY":   {"AIza123", "AIza123"},
	}
	for key, io := range ok {
		got, err := def(key).Normalize(io[0])
		if err != nil || got != io[1] {
			t.Errorf("%s(%q) = %q, %v; want %q", key, io[0], got, err, io[1])
		}
	}

	bad := map[string]string{
		"LLM_PROVIDER":    "openai",
		"CHECKIN_ENABLED": "maybe",
		"SORTIE_MINUTES":  "lots",
		"CHECKIN_TZ":      "Mars/Olympus",
		"GEMINI_API_KEY":  "two words",
		"MAX_TIER":        "deepest",
	}
	for key, in := range bad {
		if _, err := def(key).Normalize(in); !errors.Is(err, ErrInvalidValue) {
			t.Errorf("%s(%q) accepted: %v", key, in, err)
		}
	}
	if _, err := def("GEMINI_API_KEY").Normalize(""); err == nil {
		t.Error("an empty secret must be removed with null, not stored")
	}
}

func TestCatalogueNeverExposesInfrastructure(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"DATABASE_URL", "HTTP_ADDR", "SETTINGS_KEY", "ROOM_PASSWORD",
		"OIDC_CLIENT_SECRET", "OLLAMA_BASE_URL", "GEMINI_BASE_URL", "ANTHROPIC_BASE_URL", "WEB_ROOT"} {
		if Exposed(key) {
			t.Errorf("%s must come from the environment only", key)
		}
	}
}
