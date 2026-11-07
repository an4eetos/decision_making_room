// Package usecase reads and changes the settings a client may edit.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/settings/domain"
	"github.com/an4eetos/decision-room/internal/settings/port"
)

var (
	ErrUnknownKey = errors.New("unknown setting")
	ErrLocked     = errors.New("set in the server's environment, which wins; change it there")
)

// Check is one half of a provider test.
type Check struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Millis   int64  `json:"ms"`
}

// TestResult is what a provider test found.
type TestResult struct {
	Chat  Check `json:"chat"`
	Embed Check `json:"embed"`
}

// Probe makes one cheap chat call and one embedding with a configuration.
type Probe func(ctx context.Context, cfg config.Config) TestResult

// Validator checks a value the catalogue cannot judge alone, such as a
// check-in slot list.
type Validator func(value string) error

type Service struct {
	repo       port.Repository
	resolver   *config.Resolver
	cipher     *domain.Cipher // nil without SETTINGS_KEY
	validators map[string]Validator
	probe      Probe

	mu     sync.Mutex
	plain  map[string]string // stored values, secrets opened
	broken map[string]string // stored values that could not be used, and why
}

func NewService(repo port.Repository, resolver *config.Resolver, cipher *domain.Cipher, validators map[string]Validator, probe Probe) *Service {
	return &Service{
		repo:       repo,
		resolver:   resolver,
		cipher:     cipher,
		validators: validators,
		probe:      probe,
		plain:      map[string]string{},
		broken:     map[string]string{},
	}
}

// Load reads the table at boot and makes it current. A row that cannot be
// used, sealed under another key or no longer valid, is reported in the view
// and skipped, never fatal: the room must still start.
func (s *Service) Load(ctx context.Context) error {
	rows, err := s.repo.All(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	plain := map[string]string{}
	for _, row := range rows {
		if !domain.Exposed(row.Key) {
			continue
		}
		if !row.Secret {
			plain[row.Key] = row.Value
			continue
		}
		if s.cipher == nil {
			s.broken[row.Key] = domain.ErrNoSettingsKey.Error()
			continue
		}
		v, err := s.cipher.Open(row.Key, row.Value)
		if err != nil {
			s.broken[row.Key] = err.Error()
			log.Printf("settings: %s is %v and is ignored", row.Key, err)
			continue
		}
		plain[row.Key] = v
	}

	if err := s.resolver.Apply(plain); err == nil {
		s.plain = plain
		return nil
	}

	// Some stored value no longer builds, say after a change to the
	// catalogue's rules. Add them one at a time and keep what fits.
	good := map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(plain)) {
		good[key] = plain[key]
		if _, err := s.resolver.Candidate(good); err != nil {
			delete(good, key)
			s.broken[key] = err.Error()
			log.Printf("settings: stored %s ignored: %v", key, err)
		}
	}
	if err := s.resolver.Apply(good); err != nil {
		return err
	}
	s.plain = good
	return nil
}

// Item is one setting as a client shows it.
type Item struct {
	Key     string   `json:"key"`
	Group   string   `json:"group"`
	Label   string   `json:"label"`
	Help    string   `json:"help,omitempty"`
	Kind    string   `json:"kind"`
	Choices []string `json:"choices,omitempty"`
	// Value is what is in effect. A secret shows only its last four
	// characters.
	Value  string `json:"value"`
	Source string `json:"source"`
	// Locked means the environment sets it; a client cannot.
	Locked bool `json:"locked"`
	// Restart marks a setting read at boot; PendingRestart says the stored
	// value differs from the one the server is running with.
	Restart        bool   `json:"restart,omitempty"`
	PendingRestart bool   `json:"pending_restart,omitempty"`
	Problem        string `json:"problem,omitempty"`
}

// View is the whole settings page.
type View struct {
	Items []Item `json:"items"`
	// SecretsWritable is false without SETTINGS_KEY: keys must then come
	// from the environment.
	SecretsWritable bool `json:"secrets_writable"`
	// ChatProblem and EmbedProblem say what the next question will fail on.
	ChatProblem  string `json:"chat_problem,omitempty"`
	EmbedProblem string `json:"embed_problem,omitempty"`
}

func (s *Service) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view()
}

func (s *Service) view() View {
	cur, boot := s.resolver.Current(), s.resolver.Boot()
	v := View{SecretsWritable: s.cipher != nil}
	if err := cur.ChatProblem(); err != nil {
		v.ChatProblem = err.Error()
	}
	if err := cur.EmbedProblem(); err != nil {
		v.EmbedProblem = err.Error()
	}

	for _, d := range domain.Catalogue {
		_, source := s.resolver.Lookup(d.Key)
		value := effective(cur, d.Key)
		if d.Kind == domain.KindSecret && value != "" {
			value = domain.Mask(value)
		}
		v.Items = append(v.Items, Item{
			Key:            d.Key,
			Group:          d.Group,
			Label:          d.Label,
			Help:           d.Help,
			Kind:           string(d.Kind),
			Choices:        d.Choices,
			Value:          value,
			Source:         string(source),
			Locked:         source == config.SourceEnv,
			Restart:        d.Restart,
			PendingRestart: d.Restart && effective(cur, d.Key) != effective(boot, d.Key),
			Problem:        s.broken[d.Key],
		})
	}
	return v
}

// Update applies a set of changes atomically: all of them are validated
// against each other first, then saved in one transaction, then made
// current. A nil value removes the stored one, back to the default.
func (s *Service) Update(ctx context.Context, changes map[string]*string) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	next, err := s.merge(changes, true)
	if err != nil {
		return View{}, err
	}
	if _, err := s.resolver.Candidate(next); err != nil {
		return View{}, fmt.Errorf("%w: %v", domain.ErrInvalidValue, err)
	}

	var (
		upserts []port.Stored
		deletes []string
	)
	for key := range changes {
		value, keep := next[key]
		if !keep {
			deletes = append(deletes, key)
			continue
		}
		def, _ := domain.Lookup(key)
		row := port.Stored{Key: key, Value: value}
		if def.Kind == domain.KindSecret {
			sealed, err := s.cipher.Seal(key, value)
			if err != nil {
				return View{}, err
			}
			row.Value, row.Secret = sealed, true
		}
		upserts = append(upserts, row)
	}
	if err := s.repo.Save(ctx, upserts, deletes); err != nil {
		return View{}, err
	}
	if err := s.resolver.Apply(next); err != nil {
		// Unreachable: Candidate accepted the same values.
		return View{}, err
	}

	s.plain = next
	for key := range changes {
		delete(s.broken, key)
	}
	log.Printf("settings: updated %s", strings.Join(slices.Sorted(maps.Keys(changes)), ", "))
	return s.view(), nil
}

// Test probes the providers with the stored settings and the given unsaved
// changes on top, so a key can be tried before it is kept.
func (s *Service) Test(ctx context.Context, changes map[string]*string) (TestResult, error) {
	s.mu.Lock()
	next, err := s.merge(changes, false)
	s.mu.Unlock()
	if err != nil {
		return TestResult{}, err
	}
	cfg, err := s.resolver.Candidate(next)
	if err != nil {
		return TestResult{}, fmt.Errorf("%w: %v", domain.ErrInvalidValue, err)
	}
	return s.probe(ctx, cfg), nil
}

// merge validates changes and returns the stored values with them applied.
// forSaving refuses what cannot be saved: keys the environment sets, and
// secrets without SETTINGS_KEY. A test may still try those.
func (s *Service) merge(changes map[string]*string, forSaving bool) (map[string]string, error) {
	next := maps.Clone(s.plain)
	for key, value := range changes {
		def, ok := domain.Lookup(key)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownKey, key)
		}
		if forSaving && s.resolver.LockedByEnv(key) {
			return nil, fmt.Errorf("%s: %w", key, ErrLocked)
		}
		if value == nil {
			delete(next, key)
			continue
		}
		norm, err := def.Normalize(*value)
		if err != nil {
			return nil, err
		}
		if validate := s.validators[key]; validate != nil && norm != "" {
			if err := validate(norm); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", domain.ErrInvalidValue, key, err)
			}
		}
		if forSaving && def.Kind == domain.KindSecret && s.cipher == nil {
			return nil, domain.ErrNoSettingsKey
		}
		next[key] = norm
	}
	return next, nil
}

// effective is the value a key resolves to in a configuration, defaults
// and derivations included, the way a client should show it.
func effective(c config.Config, key string) string {
	switch key {
	case "LLM_PROVIDER":
		return c.LLMProvider
	case "EMBED_PROVIDER":
		return c.EmbedProvider
	case "GEMINI_API_KEY":
		return c.GeminiAPIKey
	case "GEMINI_CHAT_MODEL":
		return c.GeminiChatModel
	case "GEMINI_FALLBACK_MODEL":
		return c.GeminiFallbackModel
	case "GEMINI_EMBED_MODEL":
		return c.GeminiEmbedModel
	case "ANTHROPIC_API_KEY":
		return c.AnthropicAPIKey
	case "ANTHROPIC_CHAT_MODEL":
		return c.AnthropicChatModel
	case "ANTHROPIC_FALLBACK_MODEL":
		return c.AnthropicFallbackModel
	case "ANTHROPIC_EFFORT":
		return c.AnthropicEffort
	case "OLLAMA_CHAT_MODEL":
		return c.OllamaChatModel
	case "DEFAULT_TIER":
		return c.DefaultTier
	case "MAX_TIER":
		return c.MaxTier
	case "CHECKIN_ENABLED":
		return strconv.FormatBool(c.CheckinEnabled)
	case "CHECKIN_SLOTS":
		return c.CheckinSlots
	case "CHECKIN_TZ":
		return c.CheckinTimezone
	case "SORTIE_MINUTES":
		return strconv.Itoa(c.SortieMinutes)
	case "COMMITMENT_EXTRACTION_ENABLED":
		return strconv.FormatBool(c.CommitmentExtraction)
	case "CAMPAIGN_EXTRACTION_ENABLED":
		return strconv.FormatBool(c.CampaignExtraction)
	case "CAMPAIGN_STAFF_ENABLED":
		return strconv.FormatBool(c.CampaignStaff)
	default:
		return ""
	}
}
