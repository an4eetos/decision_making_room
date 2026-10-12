// Package anthropic talks to Claude through the official SDK. It implements the
// chat ports only: Anthropic has no embeddings endpoint, so vectors still come
// from Gemini or Ollama (EMBED_PROVIDER).
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

const (
	defaultChatModel = "claude-opus-5-5"
	// Streaming has no HTTP timeout to worry about, so it gets room to finish a
	// long answer; a blocking call stays well inside the SDK's request timeout.
	streamMaxTokens = 64000
	blockMaxTokens  = 16000
)

type Client struct {
	client     sdk.Client
	chatModels []string
	effort     string
}

// NewClient takes the model chain in failover order. effort is sent as
// output_config.effort when set; leave it empty for models that do not
// support it.
func NewClient(baseURL, apiKey string, chatModels []string, effort string) *Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if strings.TrimSpace(baseURL) != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return newClient(chatModels, effort, opts...)
}

func newClient(chatModels []string, effort string, opts ...option.RequestOption) *Client {
	return &Client{
		client:     sdk.NewClient(opts...),
		chatModels: chatModels,
		effort:     effort,
	}
}

// ErrRefused means the reply ended with stop_reason "refusal", after the
// server-side fallback model (if any) declined too. Half an answer cut off by
// a classifier is not returned as if it were the whole one.
var ErrRefused = errors.New("the model declined the request")

func (c *Client) Chat(ctx context.Context, messages []port.Message) (string, error) {
	turn, err := c.chat(ctx, messages, nil)
	if err != nil {
		return "", err
	}
	return turn.Content, nil
}

func (c *Client) ChatTools(ctx context.Context, messages []port.Message, tools []port.Tool) (port.ChatTurn, error) {
	return c.chat(ctx, messages, tools)
}

func (c *Client) ChatStream(ctx context.Context, messages []port.Message, onDelta func(string)) (string, error) {
	turn, err := c.chatStream(ctx, messages, nil, onDelta)
	if err != nil {
		return "", err
	}
	return turn.Content, nil
}

func (c *Client) ChatToolsStream(ctx context.Context, messages []port.Message, tools []port.Tool, onDelta func(string)) (port.ChatTurn, error) {
	return c.chatStream(ctx, messages, tools, onDelta)
}

func (c *Client) models() []string {
	if len(c.chatModels) == 0 {
		return []string{defaultChatModel}
	}
	return c.chatModels
}

// params builds the request for one model. port.RelaxedSafety has no
// counterpart here: Claude has no per-request harassment threshold, so beast
// mode relies on its system prompt alone.
func (c *Client) params(ctx context.Context, model string, messages []port.Message, tools []port.Tool, maxTokens int64) (sdk.BetaMessageNewParams, error) {
	system, wire, err := toWireMessages(messages)
	if err != nil {
		return sdk.BetaMessageNewParams{}, err
	}

	params := sdk.BetaMessageNewParams{
		Model:     sdk.Model(model),
		MaxTokens: maxTokens,
		System:    system,
		Messages:  wire,
		Tools:     toWireTools(tools),
		// A classifier decline is re-run server-side on the model Anthropic
		// recommends for that category, instead of failing the question.
		Betas:     []sdk.AnthropicBeta{sdk.AnthropicBetaServerSideFallback2026_07_01},
		Fallbacks: sdk.BetaFallbacksParamOfDefault(),
	}
	// The last round of a tool loop keeps the same tools and forbids calls.
	// Dropping them would change the tool set the earlier rounds' thinking
	// blocks were written against, which the API treats as an edited history;
	// and a history with tool_use blocks needs tools declared anyway.
	if len(tools) > 0 && port.ToolsWithdrawn(ctx) {
		params.ToolChoice = sdk.BetaToolChoiceUnionParam{OfNone: &sdk.BetaToolChoiceNoneParam{}}
	}
	if c.effort != "" {
		params.OutputConfig = sdk.BetaOutputConfigParam{Effort: sdk.BetaOutputConfigEffort(c.effort)}
	}
	// A tool round resends everything before it, so caching the prefix pays
	// for itself from the second round. Single-shot calls are left alone.
	if len(tools) > 0 {
		params.CacheControl = sdk.NewBetaCacheControlEphemeralParam()
	}
	return params, nil
}

func (c *Client) chat(ctx context.Context, messages []port.Message, tools []port.Tool) (port.ChatTurn, error) {
	var errOut error
	for _, model := range c.models() {
		params, err := c.params(ctx, model, messages, tools, blockMaxTokens)
		if err != nil {
			return port.ChatTurn{}, err
		}
		// The SDK already retries 429, 529 and 5xx with backoff; reaching here
		// means this model stayed unavailable, so the next one is tried.
		msg, err := c.client.Beta.Messages.New(ctx, params)
		if err == nil {
			return parseMessage(msg)
		}
		errOut = err
		if !isRetryableModelError(err) {
			return port.ChatTurn{}, err
		}
	}
	return port.ChatTurn{}, errOut
}

// chatStream fails over to the next model only until the first event arrives:
// once text has reached the reader it cannot be taken back.
func (c *Client) chatStream(ctx context.Context, messages []port.Message, tools []port.Tool, onDelta func(string)) (port.ChatTurn, error) {
	var errOut error
	for _, model := range c.models() {
		params, err := c.params(ctx, model, messages, tools, streamMaxTokens)
		if err != nil {
			return port.ChatTurn{}, err
		}

		stream := c.client.Beta.Messages.NewStreaming(ctx, params)
		if !stream.Next() {
			err := stream.Err()
			stream.Close()
			if err == nil {
				return port.ChatTurn{}, fmt.Errorf("empty message stream")
			}
			errOut = err
			if !isRetryableModelError(err) {
				return port.ChatTurn{}, err
			}
			continue
		}
		defer stream.Close()

		var msg sdk.BetaMessage
		for {
			event := stream.Current()
			if err := msg.Accumulate(event); err != nil {
				return port.ChatTurn{}, fmt.Errorf("accumulate stream: %w", err)
			}
			// A mid-stream refusal fallback keeps what was already streamed
			// and the next model carries on from it, so every text delta is
			// final as soon as it arrives.
			if delta, ok := event.AsAny().(sdk.BetaRawContentBlockDeltaEvent); ok && onDelta != nil {
				if text, ok := delta.Delta.AsAny().(sdk.BetaTextDelta); ok && text.Text != "" {
					onDelta(text.Text)
				}
			}
			if !stream.Next() {
				break
			}
		}
		if err := stream.Err(); err != nil {
			return port.ChatTurn{}, fmt.Errorf("read stream: %w", err)
		}
		return parseMessage(&msg)
	}
	return port.ChatTurn{}, errOut
}

// isRetryableModelError covers rate limiting (429), overload (529) and server
// errors, the cases where another model may well succeed.
func isRetryableModelError(err error) bool {
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		529:
		return true
	default:
		return false
	}
}

func parseMessage(msg *sdk.BetaMessage) (port.ChatTurn, error) {
	if msg.StopReason == sdk.BetaStopReasonRefusal {
		if category := string(msg.StopDetails.Category); category != "" {
			return port.ChatTurn{}, fmt.Errorf("%w (%s)", ErrRefused, category)
		}
		return port.ChatTurn{}, ErrRefused
	}

	// After a mid-output fallback, reasoning and tool calls from before the
	// last switch belong to the model that declined and must not be replayed;
	// text before it is the start of the answer and is kept.
	boundary := -1
	for i, block := range msg.Content {
		if block.Type == "fallback" {
			boundary = i
		}
	}

	var content strings.Builder
	toolCalls := make([]port.ToolCall, 0)
	parts := make([]port.ContentPart, 0, len(msg.Content))
	for i, block := range msg.Content {
		switch block.Type {
		case "text":
			content.WriteString(block.Text)
			parts = append(parts, port.ContentPart{Text: block.Text})
		case "thinking":
			if i < boundary {
				continue
			}
			parts = append(parts, port.ContentPart{Reasoning: &port.Reasoning{
				Thinking:  block.Thinking,
				Signature: block.Signature,
			}})
		case "redacted_thinking":
			if i < boundary {
				continue
			}
			parts = append(parts, port.ContentPart{Reasoning: &port.Reasoning{Data: block.Data}})
		case "tool_use":
			if i < boundary {
				continue
			}
			args, err := parseArguments(block.Input)
			if err != nil {
				return port.ChatTurn{}, fmt.Errorf("tool %s: %w", block.Name, err)
			}
			call := port.ToolCall{ID: block.ID, Name: block.Name, Arguments: args}
			toolCalls = append(toolCalls, call)
			parts = append(parts, port.ContentPart{ToolCall: &call})
		}
	}

	turn := port.ChatTurn{
		Content:   strings.TrimSpace(content.String()),
		ToolCalls: toolCalls,
		Parts:     parts,
	}
	if turn.Content == "" && len(turn.ToolCalls) == 0 {
		return port.ChatTurn{}, fmt.Errorf("empty message (stop reason %q)", msg.StopReason)
	}
	return turn, nil
}

func parseArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("parse tool input: %w", err)
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

// toWireMessages maps the port's conversation onto the Messages API. System
// messages become the system prompt; a tool result is a tool_result block in a
// user turn. Consecutive turns of the same role are merged, which is how the
// results of parallel calls end up in the single user turn the API expects,
// and how the context block, history and question sit together.
func toWireMessages(messages []port.Message) ([]sdk.BetaTextBlockParam, []sdk.BetaMessageParam, error) {
	var system []sdk.BetaTextBlockParam
	var out []sdk.BetaMessageParam
	// Calls from the latest assistant turn not yet answered, so a tool message
	// without an ID can still be paired, in order.
	var pending []string

	push := func(role sdk.BetaMessageParamRole, blocks ...sdk.BetaContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			return
		}
		out = append(out, sdk.BetaMessageParam{Role: role, Content: blocks})
	}

	for _, message := range messages {
		switch message.Role {
		case "system":
			if strings.TrimSpace(message.Content) != "" {
				system = append(system, sdk.BetaTextBlockParam{Text: message.Content})
			}
		case "user":
			if strings.TrimSpace(message.Content) != "" {
				push(sdk.BetaMessageParamRoleUser, sdk.NewBetaTextBlock(message.Content))
			}
		case "assistant":
			blocks, ids := assistantBlocks(message)
			pending = ids
			push(sdk.BetaMessageParamRoleAssistant, blocks...)
		case "tool":
			id := message.ToolCallID
			if id == "" && len(pending) > 0 {
				id = pending[0]
			}
			if id == "" {
				return nil, nil, fmt.Errorf("tool message for %q has no call to answer", message.ToolName)
			}
			pending = removeID(pending, id)
			push(sdk.BetaMessageParamRoleUser, sdk.NewBetaToolResultBlock(id, message.Content, false))
		default:
			return nil, nil, fmt.Errorf("unsupported message role: %s", message.Role)
		}
	}

	return system, out, nil
}

// assistantBlocks replays a turn this client produced from its parts, in their
// original order, so thinking blocks stay attached to the calls they preceded.
// A turn stored as plain content (chat history) has no parts. Calls without an
// ID are given one, since the API pairs results to calls by it.
func assistantBlocks(message port.Message) ([]sdk.BetaContentBlockParamUnion, []string) {
	var blocks []sdk.BetaContentBlockParamUnion
	var ids []string

	addCall := func(call port.ToolCall) {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("toolu_local_%d", len(ids))
		}
		ids = append(ids, id)
		args := call.Arguments
		if args == nil {
			args = map[string]any{}
		}
		blocks = append(blocks, sdk.NewBetaToolUseBlock(id, args, call.Name))
	}

	if len(message.Parts) > 0 {
		for _, part := range message.Parts {
			switch {
			case part.Reasoning != nil && part.Reasoning.Data != "":
				blocks = append(blocks, sdk.NewBetaRedactedThinkingBlock(part.Reasoning.Data))
			case part.Reasoning != nil:
				blocks = append(blocks, sdk.NewBetaThinkingBlock(part.Reasoning.Signature, part.Reasoning.Thinking))
			case part.ToolCall != nil:
				addCall(*part.ToolCall)
			case part.Text != "":
				blocks = append(blocks, sdk.NewBetaTextBlock(part.Text))
			}
		}
		return blocks, ids
	}

	if strings.TrimSpace(message.Content) != "" {
		blocks = append(blocks, sdk.NewBetaTextBlock(message.Content))
	}
	for _, call := range message.ToolCalls {
		addCall(call)
	}
	return blocks, ids
}

func removeID(ids []string, id string) []string {
	for i, v := range ids {
		if v == id {
			return append(ids[:i:i], ids[i+1:]...)
		}
	}
	return ids
}

// toWireTools passes the port's JSON Schema through. Properties, required and
// type are typed fields on the SDK side; anything else rides along as extras.
func toWireTools(tools []port.Tool) []sdk.BetaToolUnionParam {
	if len(tools) == 0 {
		return nil
	}

	out := make([]sdk.BetaToolUnionParam, len(tools))
	for i, tool := range tools {
		schema := sdk.BetaToolInputSchemaParam{Properties: map[string]any{}}
		extras := map[string]any{}
		for key, value := range tool.Parameters {
			switch key {
			case "type":
			case "properties":
				schema.Properties = value
			case "required":
				schema.Required = toStrings(value)
			default:
				extras[key] = value
			}
		}
		if len(extras) > 0 {
			schema.ExtraFields = extras
		}

		param := sdk.BetaToolParam{Name: tool.Name, InputSchema: schema}
		if tool.Description != "" {
			param.Description = sdk.String(tool.Description)
		}
		out[i] = sdk.BetaToolUnionParam{OfTool: &param}
	}
	return out
}

func toStrings(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
