package config

import "testing"

func exposedForTest(key string) bool {
	switch key {
	case "GEMINI_API_KEY", "LLM_PROVIDER", "SORTIE_MINUTES", "ANTHROPIC_EFFORT":
		return true
	}
	return false
}

func newTestResolver(t *testing.T, env map[string]string) *Resolver {
	t.Helper()
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	boot, err := Build(lookup)
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(boot, exposedForTest)
	r.env = lookup
	return r
}

func TestResolverPrecedence(t *testing.T) {
	t.Parallel()
	r := newTestResolver(t, map[string]string{
		"SORTIE_MINUTES":   "30",
		"GEMINI_API_KEY":   "", // as .env.example ships it: must not lock the key
		"ANTHROPIC_EFFORT": "", // set-but-empty means disabled: locks
	})

	var seen []Config
	r.OnChange(func(c Config) { seen = append(seen, c) })

	if err := r.Apply(map[string]string{
		"GEMINI_API_KEY":   "stored-key",
		"SORTIE_MINUTES":   "90",
		"ANTHROPIC_EFFORT": "max",
		"DATABASE_URL":     "postgres://evil", // not exposed: never read
	}); err != nil {
		t.Fatal(err)
	}
	cur := r.Current()

	if cur.GeminiAPIKey != "stored-key" {
		t.Errorf("empty env should fall through to stored: %q", cur.GeminiAPIKey)
	}
	if cur.SortieMinutes != 30 {
		t.Errorf("env should beat stored: %d", cur.SortieMinutes)
	}
	if cur.AnthropicEffort != "" {
		t.Errorf("set-but-empty env should lock: %q", cur.AnthropicEffort)
	}
	if cur.DatabaseURL == "postgres://evil" {
		t.Error("an unexposed key was read from the table")
	}
	if len(seen) != 1 || seen[0].GeminiAPIKey != "stored-key" {
		t.Errorf("listener saw %d changes", len(seen))
	}

	if v, src := r.Lookup("SORTIE_MINUTES"); src != SourceEnv || v != "30" {
		t.Errorf("SORTIE_MINUTES: %q %s", v, src)
	}
	if _, src := r.Lookup("GEMINI_API_KEY"); src != SourceStored {
		t.Errorf("GEMINI_API_KEY source %s", src)
	}
	if _, src := r.Lookup("LLM_PROVIDER"); src != SourceDefault {
		t.Errorf("LLM_PROVIDER source %s", src)
	}
	if !r.LockedByEnv("ANTHROPIC_EFFORT") || r.LockedByEnv("GEMINI_API_KEY") {
		t.Error("LockedByEnv wrong")
	}
}

func TestResolverRejectsInvalidAndKeepsCurrent(t *testing.T) {
	t.Parallel()
	r := newTestResolver(t, map[string]string{})
	before := r.Current()

	if err := r.Apply(map[string]string{"LLM_PROVIDER": "openai"}); err == nil {
		t.Fatal("invalid provider applied")
	}
	if r.Current().LLMProvider != before.LLMProvider {
		t.Fatal("a failed Apply changed the configuration")
	}
}

func TestResolverSettle(t *testing.T) {
	t.Parallel()
	r := newTestResolver(t, map[string]string{})
	_ = r.Apply(map[string]string{"SORTIE_MINUTES": "45"})
	if got := r.Settle().SortieMinutes; got != 45 {
		t.Fatalf("Settle: %d", got)
	}
	_ = r.Apply(map[string]string{"SORTIE_MINUTES": "60"})
	if r.Boot().SortieMinutes != 45 || r.Current().SortieMinutes != 60 {
		t.Fatal("Boot must stay what the process settled on")
	}
}
