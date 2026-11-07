package config

import (
	"log"
	"os"
	"sync"
)

// Source says where a setting's value came from.
type Source string

const (
	SourceEnv     Source = "env"     // the process environment or .env; a client cannot change it
	SourceStored  Source = "stored"  // typed into a client and kept in the settings table
	SourceDefault Source = "default" // neither
)

// Resolver answers "what is the configuration right now". The order is the
// environment (with .env), then the settings table, then the default. It is
// read at call time, so a key typed in a client is used by the next question
// without a restart.
//
// Only keys the settings catalogue exposes are ever read from the table;
// DATABASE_URL and the like come from the environment or nowhere.
type Resolver struct {
	env     Lookup
	exposed func(key string) bool
	boot    Config

	mu        sync.RWMutex
	stored    map[string]string
	current   Config
	listeners []func(Config)
}

// NewResolver starts from the configuration the process booted with.
// exposed decides which keys the table may supply.
func NewResolver(boot Config, exposed func(key string) bool) *Resolver {
	return &Resolver{
		env:     os.LookupEnv,
		exposed: exposed,
		boot:    boot,
		stored:  map[string]string{},
		current: boot,
	}
}

// Current is the configuration to use for this call.
func (r *Resolver) Current() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.current
}

// Settle marks the configuration the process runs with from here on, once
// the stored settings are loaded, and returns it. Constructors that read a
// setting once (the check-in timezone) get this, so a stored value applies
// from the next restart.
func (r *Resolver) Settle() Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.boot = r.current
	return r.boot
}

// Boot is the configuration the process started with, for settings that only
// take effect on a restart.
func (r *Resolver) Boot() Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.boot
}

// Candidate builds the configuration that a set of stored values would give,
// without applying it: a PUT is validated this way before it is saved.
func (r *Resolver) Candidate(stored map[string]string) (Config, error) {
	return Build(r.layered(stored))
}

// Apply makes stored values current and tells every listener. The values are
// validated first; on error nothing changes.
func (r *Resolver) Apply(stored map[string]string) error {
	cfg, err := r.Candidate(stored)
	if err != nil {
		return err
	}
	copied := make(map[string]string, len(stored))
	for k, v := range stored {
		copied[k] = v
	}

	r.mu.Lock()
	r.stored = copied
	r.current = cfg
	listeners := append([]func(Config){}, r.listeners...)
	r.mu.Unlock()

	for _, fn := range listeners {
		fn(cfg)
	}
	return nil
}

// OnChange registers fn to run after every Apply, with the new configuration.
func (r *Resolver) OnChange(fn func(Config)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listeners = append(r.listeners, fn)
}

// Lookup reports the raw value a key resolves to and where it came from.
func (r *Resolver) Lookup(key string) (string, Source) {
	if v, ok := r.fromEnv(key); ok {
		return v, SourceEnv
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if v, ok := r.stored[key]; ok && r.exposed(key) {
		return v, SourceStored
	}
	return "", SourceDefault
}

// LockedByEnv reports whether the environment sets a key, which then wins
// over anything a client stores.
func (r *Resolver) LockedByEnv(key string) bool {
	_, ok := r.fromEnv(key)
	return ok
}

func (r *Resolver) layered(stored map[string]string) Lookup {
	return func(key string) (string, bool) {
		if v, ok := r.fromEnv(key); ok {
			return v, true
		}
		if r.exposed(key) {
			if v, ok := stored[key]; ok {
				return v, true
			}
		}
		return "", false
	}
}

// fromEnv treats an empty variable as unset, the way Build does for most
// keys: .env.example ships "GEMINI_API_KEY=", and that must not lock the key
// against being typed in a client. The keys where set-but-empty means
// "disabled" count as set.
func (r *Resolver) fromEnv(key string) (string, bool) {
	v, ok := r.env(key)
	if !ok {
		return "", false
	}
	if v == "" && !emptyMeansDisabled[key] {
		return "", false
	}
	return v, true
}

// emptyMeansDisabled lists the keys Build reads with orAllowEmpty.
var emptyMeansDisabled = map[string]bool{
	"ANTHROPIC_FALLBACK_MODEL": true,
	"ANTHROPIC_EFFORT":         true,
}

// LogProblems reports at boot what will fail on the first question, so a
// missing key is visible in the log and not only in the answer.
func LogProblems(cfg Config) {
	if err := cfg.ChatProblem(); err != nil {
		log.Printf("config: %v", err)
	}
	if err := cfg.EmbedProblem(); err != nil {
		log.Printf("config: %v", err)
	}
}
