// Package aiswitch puts the chat and embedding providers behind one adapter
// that reads the configuration on every call. Switching provider, model or
// key in Settings takes effect on the next question, and nothing that holds a
// port.LLM or port.Embedder has to know it can change.
package aiswitch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/an4eetos/decision-room/internal/infra/anthropic"
	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/infra/gemini"
	"github.com/an4eetos/decision-room/internal/infra/ollama"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// client is what every provider implements for chat.
type client interface {
	port.ToolLLM
	port.StreamingLLM
}

// Switch is a port.ToolLLM, port.StreamingLLM and port.Embedder whose
// provider is whatever the configuration says at the moment of the call.
//
// One client per provider is kept and rebuilt only when its own settings
// change, so a provider's failover state survives while nothing about it
// changes. When chat and embeddings both use Gemini with the same settings,
// they share the one client, as they did before.
type Switch struct {
	current func() config.Config

	mu  sync.Mutex
	gem cached[*gemini.Client]
	ant cached[*anthropic.Client]
	oll cached[*ollama.Client]
}

type cached[T any] struct {
	fingerprint string
	client      T
}

func New(current func() config.Config) *Switch {
	return &Switch{current: current}
}

func (s *Switch) Chat(ctx context.Context, messages []port.Message) (string, error) {
	c, err := s.chatClient(s.current())
	if err != nil {
		return "", err
	}
	return c.Chat(ctx, messages)
}

func (s *Switch) ChatTools(ctx context.Context, messages []port.Message, tools []port.Tool) (port.ChatTurn, error) {
	c, err := s.chatClient(s.current())
	if err != nil {
		return port.ChatTurn{}, err
	}
	return c.ChatTools(ctx, messages, tools)
}

func (s *Switch) ChatStream(ctx context.Context, messages []port.Message, onDelta func(string)) (string, error) {
	c, err := s.chatClient(s.current())
	if err != nil {
		return "", err
	}
	return c.ChatStream(ctx, messages, onDelta)
}

func (s *Switch) ChatToolsStream(ctx context.Context, messages []port.Message, tools []port.Tool, onDelta func(string)) (port.ChatTurn, error) {
	c, err := s.chatClient(s.current())
	if err != nil {
		return port.ChatTurn{}, err
	}
	return c.ChatToolsStream(ctx, messages, tools, onDelta)
}

// ModelID needs no key: it names the model, so vectors can be labelled and
// the mismatch guard can run before a key is ever entered.
func (s *Switch) ModelID() string {
	return s.embedder(s.current()).ModelID()
}

func (s *Switch) Embed(ctx context.Context, text string) ([]float32, error) {
	cfg := s.current()
	if err := cfg.EmbedProblem(); err != nil {
		return nil, err
	}
	return s.embedder(cfg).Embed(ctx, text)
}

func (s *Switch) chatClient(cfg config.Config) (client, error) {
	if err := cfg.ChatProblem(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch cfg.LLMProvider {
	case "anthropic":
		return s.anthropic(cfg), nil
	case "gemini":
		return s.gemini(cfg), nil
	default:
		return s.ollama(cfg), nil
	}
}

func (s *Switch) embedder(cfg config.Config) port.Embedder {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cfg.EmbedProvider == "gemini" {
		return s.gemini(cfg)
	}
	return s.ollama(cfg)
}

// The builders below run under s.mu.

func (s *Switch) gemini(cfg config.Config) *gemini.Client {
	fp := strings.Join([]string{cfg.GeminiBaseURL, cfg.GeminiAPIKey, strings.Join(cfg.GeminiChatModels, ","), cfg.GeminiEmbedModel}, "\x00")
	if s.gem.client == nil || s.gem.fingerprint != fp {
		s.gem = cached[*gemini.Client]{fp, gemini.NewClient(cfg.GeminiBaseURL, cfg.GeminiAPIKey, cfg.GeminiChatModels, cfg.GeminiEmbedModel)}
	}
	return s.gem.client
}

func (s *Switch) anthropic(cfg config.Config) *anthropic.Client {
	fp := strings.Join([]string{cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, strings.Join(cfg.AnthropicChatModels, ","), cfg.AnthropicEffort}, "\x00")
	if s.ant.client == nil || s.ant.fingerprint != fp {
		s.ant = cached[*anthropic.Client]{fp, anthropic.NewClient(cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, cfg.AnthropicChatModels, cfg.AnthropicEffort)}
	}
	return s.ant.client
}

func (s *Switch) ollama(cfg config.Config) *ollama.Client {
	fp := strings.Join([]string{cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaEmbedModel}, "\x00")
	if s.oll.client == nil || s.oll.fingerprint != fp {
		s.oll = cached[*ollama.Client]{fp, ollama.NewClient(cfg.OllamaBaseURL, cfg.OllamaChatModel, cfg.OllamaEmbedModel)}
	}
	return s.oll.client
}

// probeTimeout bounds a settings test; a provider slower than this to say
// "OK" will feel broken anyway.
const probeTimeout = 30 * time.Second

// Check is one half of a probe.
type Check struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Millis   int64  `json:"ms"`
}

// Probe makes one tiny chat call and one embedding with a configuration that
// may not be saved yet, through fresh clients so nothing it tries is cached.
// The errors are the providers' own, which is the point: "API key not valid"
// from Google says more than anything the room could write.
func Probe(ctx context.Context, cfg config.Config) (chat, embed Check) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	scratch := New(func() config.Config { return cfg })

	chat = Check{Provider: cfg.LLMProvider, Model: chatModel(cfg)}
	start := time.Now()
	reply, err := scratch.Chat(ctx, []port.Message{{Role: "user", Content: "Reply with the single word OK."}})
	chat.Millis = time.Since(start).Milliseconds()
	switch {
	case err != nil:
		chat.Error = err.Error()
	case strings.TrimSpace(reply) == "":
		chat.Error = errors.New("the model answered with nothing").Error()
	default:
		chat.OK = true
	}

	embed = Check{Provider: cfg.EmbedProvider, Model: scratch.ModelID()}
	start = time.Now()
	vec, err := scratch.Embed(ctx, "probe")
	embed.Millis = time.Since(start).Milliseconds()
	switch {
	case err != nil:
		embed.Error = err.Error()
	case len(vec) == 0:
		embed.Error = "the embedder returned an empty vector"
	default:
		embed.OK = true
	}
	return chat, embed
}

func chatModel(cfg config.Config) string {
	switch cfg.LLMProvider {
	case "anthropic":
		return strings.Join(cfg.AnthropicChatModels, ", ")
	case "gemini":
		return strings.Join(cfg.GeminiChatModels, ", ")
	default:
		return cfg.OllamaChatModel
	}
}
