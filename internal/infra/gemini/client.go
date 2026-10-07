package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

const (
	defaultBaseURL   = "https://generativelanguage.googleapis.com/v1beta"
	defaultChatModel = "gemini-flash-latest"
	// embedDimensions matches the vector(768) column, and nomic-embed-text, so
	// either provider can fill the same table shape.
	embedDimensions = 768
	// Transient overload is common on the free tier and lasts seconds. Failing a
	// whole question because of it is worse than waiting.
	maxRateRetries = 2
	// A retry that waits longer than this is not worth it interactively; better
	// to surface the error than to hang.
	maxRetryDelay = 20 * time.Second
)

type Client struct {
	baseURL    string
	apiKey     string
	chatModels []string
	embedModel string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string, chatModels []string, embedModel string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		chatModels: chatModels,
		embedModel: embedModel,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

type generateRequest struct {
	SystemInstruction *contentWire    `json:"systemInstruction,omitempty"`
	Contents          []contentWire   `json:"contents"`
	Tools             []toolGroupWire `json:"tools,omitempty"`
}

type contentWire struct {
	Role  string     `json:"role"`
	Parts []partWire `json:"parts"`
}

type partWire struct {
	Text             string                `json:"text,omitempty"`
	FunctionCall     *functionCallWire     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponseWire `json:"functionResponse,omitempty"`
	ThoughtSignature string                `json:"thoughtSignature,omitempty"`
}

type functionCallWire struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type functionResponseWire struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type toolGroupWire struct {
	FunctionDeclarations []functionDeclWire `json:"functionDeclarations"`
}

type functionDeclWire struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type generateResponse struct {
	Candidates []struct {
		Content contentWire `json:"content"`
	} `json:"candidates"`
}

type embedRequest struct {
	Content              contentWire `json:"content"`
	OutputDimensionality int         `json:"output_dimensionality,omitempty"`
}

type embedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

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

func (c *Client) generateBody(messages []port.Message, tools []port.Tool) ([]byte, error) {
	systemInstruction, contents, err := toWireContents(messages)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(generateRequest{
		SystemInstruction: systemInstruction,
		Contents:          contents,
		Tools:             toWireTools(tools),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal generate request: %w", err)
	}
	return body, nil
}

func (c *Client) models() []string {
	if len(c.chatModels) == 0 {
		// An alias rather than a pinned version: pinned ones get retired, and the
		// app then fails every question with "this model is no longer available".
		return []string{defaultChatModel}
	}
	return c.chatModels
}

// chatStream has the same retry and failover as chat, but only until the first
// byte of a successful response: once text has reached the reader it cannot be
// taken back, so a failure mid-stream is returned rather than retried.
func (c *Client) chatStream(ctx context.Context, messages []port.Message, tools []port.Tool, onDelta func(string)) (port.ChatTurn, error) {
	body, err := c.generateBody(messages, tools)
	if err != nil {
		return port.ChatTurn{}, err
	}

	var resp *http.Response
	var errOut error
	for _, model := range c.models() {
		url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", c.baseURL, normalizeModel(model))
		resp, errOut = c.send(ctx, url, body)
		if errOut == nil {
			break
		}
		if !isRetryableModelError(errOut) {
			return port.ChatTurn{}, errOut
		}
	}
	if errOut != nil {
		return port.ChatTurn{}, errOut
	}
	defer resp.Body.Close()

	return readStream(resp.Body, onDelta)
}

// maxStreamLine bounds one SSE event. A chunk carrying a large tool call can
// run well past bufio's 64KB default.
const maxStreamLine = 4 << 20

type streamChunk struct {
	generateResponse
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// readStream consumes a streamGenerateContent SSE body. Text is handed to
// onDelta as it arrives; the parts are reassembled into one turn so the result
// is interchangeable with the non-streaming call's.
func readStream(r io.Reader, onDelta func(string)) (port.ChatTurn, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxStreamLine)

	var parts []partWire
	for scanner.Scan() {
		line := scanner.Text()
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "" {
			continue
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return port.ChatTurn{}, fmt.Errorf("unmarshal stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return port.ChatTurn{}, fmt.Errorf("stream failed: %s", chunk.Error.Message)
		}
		if len(chunk.Candidates) == 0 {
			continue
		}

		for _, part := range chunk.Candidates[0].Content.Parts {
			if part.Text != "" && onDelta != nil {
				onDelta(part.Text)
			}
			parts = appendStreamPart(parts, part)
		}
	}
	if err := scanner.Err(); err != nil {
		return port.ChatTurn{}, fmt.Errorf("read stream: %w", err)
	}

	if len(parts) == 0 {
		return port.ChatTurn{}, fmt.Errorf("empty generate response")
	}
	return parseModelTurn(parts)
}

// appendStreamPart folds a streamed part into the turn. Text arrives a few words
// per chunk and is joined back into one part; a thought signature can arrive on
// its own in a trailing empty part and belongs to the text before it. Function
// calls always stand alone.
func appendStreamPart(parts []partWire, part partWire) []partWire {
	if part.FunctionCall != nil {
		return append(parts, part)
	}

	var last *partWire
	if n := len(parts); n > 0 && parts[n-1].FunctionCall == nil && parts[n-1].ThoughtSignature == "" {
		last = &parts[n-1]
	}

	switch {
	case part.Text != "" && last != nil:
		last.Text += part.Text
		last.ThoughtSignature = part.ThoughtSignature
	case part.Text == "" && part.ThoughtSignature != "" && last != nil:
		last.ThoughtSignature = part.ThoughtSignature
	case part.Text != "" || part.ThoughtSignature != "":
		parts = append(parts, part)
	}
	return parts
}

func (c *Client) chat(ctx context.Context, messages []port.Message, tools []port.Tool) (port.ChatTurn, error) {
	body, err := c.generateBody(messages, tools)
	if err != nil {
		return port.ChatTurn{}, err
	}

	var respBody []byte
	var errOut error
	for _, model := range c.models() {
		url := fmt.Sprintf("%s/models/%s:generateContent", c.baseURL, normalizeModel(model))
		// Retries within a model first, then falls over to the next one. Chat used
		// to skip retrying entirely, so a momentary spike killed the request even
		// though the error was explicitly classified as retryable.
		respBody, errOut = c.post(ctx, url, body)
		if errOut == nil {
			break
		}
		if !isRetryableModelError(errOut) {
			return port.ChatTurn{}, errOut
		}
	}
	if errOut != nil {
		return port.ChatTurn{}, errOut
	}

	var result generateResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return port.ChatTurn{}, fmt.Errorf("unmarshal generate response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return port.ChatTurn{}, fmt.Errorf("empty generate response")
	}

	return parseModelTurn(result.Candidates[0].Content.Parts)
}

// ModelID includes the output dimensionality, because the same model truncated
// to a different width produces vectors that are not comparable.
func (c *Client) ModelID() string {
	return fmt.Sprintf("gemini:%s@%d", normalizeModel(c.embedModel), embedDimensions)
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{
		Content: contentWire{
			Parts: []partWire{{Text: text}},
		},
		OutputDimensionality: embedDimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:embedContent", c.baseURL, normalizeModel(c.embedModel))
	respBody, err := c.post(ctx, url, body)
	if err != nil {
		return nil, err
	}

	var result embedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal embed response: %w", err)
	}

	if len(result.Embedding.Values) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return result.Embedding.Values, nil
}

func (c *Client) post(ctx context.Context, url string, body []byte) ([]byte, error) {
	resp, err := c.send(ctx, url, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return respBody, nil
}

// send posts with retries and returns the open body of the first 200. The
// caller closes it. Split from post so a stream can be read as it arrives
// while sharing the same retry behaviour.
func (c *Client) send(ctx context.Context, url string, body []byte) (*http.Response, error) {
	var (
		lastBody   []byte
		lastStatus int
	)

	for attempt := 0; attempt <= maxRateRetries; attempt++ {
		resp, err := c.doPost(ctx, url, body)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}

		lastBody, lastStatus = respBody, resp.StatusCode
		if !isRetryableStatus(resp.StatusCode) || attempt == maxRateRetries {
			break
		}

		delay := retryDelayFor(respBody, attempt)
		if delay <= 0 {
			break
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, &apiError{status: lastStatus, message: c.redactString(formatAPIError(lastBody))}
}

// apiError carries the HTTP status alongside the message.
//
// Retry and failover decisions used to be made by substring-matching the
// human-readable error text, which silently failed for quota exhaustion — the
// message there is "You exceeded your current quota", containing none of the
// markers being looked for, so the fallback model was never tried in the one
// case it exists for. The status code was available the whole time.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("request failed (%d): %s", e.status, e.message)
}

func (e *apiError) Retryable() bool { return isRetryableStatus(e.status) }

// isRetryableStatus covers more than rate limiting. Overload is reported as a
// 500 or 503 with a "high demand" message, not a 429, so retrying only on 429
// meant the most common transient failure was never retried at all.
func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// retryDelayFor honours the server's own RetryInfo when it gives one, and backs
// off exponentially otherwise. It returns zero when the wait would be too long
// to be worth it, which the caller treats as "give up now".
func retryDelayFor(respBody []byte, attempt int) time.Duration {
	if delay := parseRetryDelay(respBody); delay > 0 {
		if delay > maxRetryDelay {
			return 0
		}
		return delay
	}
	return time.Duration(1<<attempt) * time.Second
}

// doPost sends the key in the x-goog-api-key header, never in the URL. Go's
// HTTP errors quote the request URL, so a key in the query string was written
// to the log by every timeout, refused connection and DNS failure.
func (c *Client) doPost(ctx context.Context, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", c.redact(err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", c.redact(err))
	}
	return resp, nil
}

// redact is the second line: should the key reach an error by another route —
// a misconfigured base URL that embeds it, a proxy that echoes it — it still
// never reaches a log.
func (c *Client) redact(err error) error {
	if err == nil || c.apiKey == "" || !strings.Contains(err.Error(), c.apiKey) {
		return err
	}
	return redactedError{msg: c.redactString(err.Error()), cause: err}
}

func (c *Client) redactString(s string) string {
	if c.apiKey == "" {
		return s
	}
	return strings.ReplaceAll(s, c.apiKey, "[REDACTED]")
}

// redactedError keeps the cause for errors.Is and errors.As — a cancelled
// context must still read as cancelled — while its message hides the key.
type redactedError struct {
	msg   string
	cause error
}

func (e redactedError) Error() string { return e.msg }
func (e redactedError) Unwrap() error { return e.cause }

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Details []struct {
			Type       string `json:"@type"`
			RetryDelay string `json:"retryDelay"`
		} `json:"details"`
	} `json:"error"`
}

func parseRetryDelay(body []byte) time.Duration {
	var parsed apiErrorResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0
	}

	for _, detail := range parsed.Error.Details {
		if !strings.Contains(detail.Type, "RetryInfo") || detail.RetryDelay == "" {
			continue
		}
		if d, err := time.ParseDuration(detail.RetryDelay); err == nil {
			return d
		}
	}

	return 0
}

func formatAPIError(body []byte) string {
	var parsed apiErrorResponse
	if err := json.Unmarshal(body, &parsed); err == nil && strings.TrimSpace(parsed.Error.Message) != "" {
		return parsed.Error.Message
	}
	return string(body)
}

// isRetryableModelError reports whether failing over to the next model is worth
// trying. It prefers the HTTP status and falls back to matching the message only
// for errors that did not come from an API response.
func isRetryableModelError(err error) bool {
	if err == nil {
		return false
	}

	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr.Retryable()
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "status 429"),
		strings.Contains(message, "resource_exhausted"),
		strings.Contains(message, "high demand"),
		strings.Contains(message, "overloaded"),
		strings.Contains(message, "temporarily unavailable"),
		strings.Contains(message, "status 500"),
		strings.Contains(message, "status 503"),
		strings.Contains(message, "exceeded your current quota"):
		return true
	default:
		return false
	}
}

func toWireContents(messages []port.Message) (*contentWire, []contentWire, error) {
	var systemInstruction *contentWire
	contents := make([]contentWire, 0, len(messages))

	for _, message := range messages {
		switch message.Role {
		case "system":
			systemInstruction = &contentWire{
				Parts: []partWire{{Text: message.Content}},
			}
		case "user":
			contents = append(contents, contentWire{
				Role:  "user",
				Parts: []partWire{{Text: message.Content}},
			})
		case "assistant":
			if len(message.Parts) > 0 {
				parts := make([]partWire, len(message.Parts))
				for i, part := range message.Parts {
					wire := partWire{ThoughtSignature: part.ThoughtSignature}
					if part.Text != "" {
						wire.Text = part.Text
					}
					if part.ToolCall != nil {
						wire.FunctionCall = &functionCallWire{
							Name: part.ToolCall.Name,
							Args: part.ToolCall.Arguments,
						}
					}
					parts[i] = wire
				}
				contents = append(contents, contentWire{Role: "model", Parts: parts})
				break
			}

			parts := make([]partWire, 0, 1+len(message.ToolCalls))
			if strings.TrimSpace(message.Content) != "" {
				parts = append(parts, partWire{Text: message.Content})
			}
			for _, call := range message.ToolCalls {
				parts = append(parts, partWire{
					FunctionCall: &functionCallWire{
						Name: call.Name,
						Args: call.Arguments,
					},
				})
			}
			contents = append(contents, contentWire{Role: "model", Parts: parts})
		case "tool":
			toolName := message.ToolName
			if toolName == "" {
				return nil, nil, fmt.Errorf("tool message missing tool name")
			}
			contents = append(contents, contentWire{
				Role: "user",
				Parts: []partWire{{
					FunctionResponse: &functionResponseWire{
						Name: toolName,
						Response: map[string]any{
							"content": message.Content,
						},
					},
				}},
			})
		default:
			return nil, nil, fmt.Errorf("unsupported message role: %s", message.Role)
		}
	}

	return systemInstruction, contents, nil
}

func parseModelTurn(parts []partWire) (port.ChatTurn, error) {
	var content strings.Builder
	toolCalls := make([]port.ToolCall, 0)
	contentParts := make([]port.ContentPart, 0, len(parts))

	for _, part := range parts {
		contentPart := port.ContentPart{ThoughtSignature: part.ThoughtSignature}

		if part.Text != "" {
			content.WriteString(part.Text)
			contentPart.Text = part.Text
			contentParts = append(contentParts, contentPart)
		}

		if part.FunctionCall != nil {
			call := port.ToolCall{
				Name:      part.FunctionCall.Name,
				Arguments: part.FunctionCall.Args,
			}
			toolCalls = append(toolCalls, call)
			contentPart.ToolCall = &call
			contentParts = append(contentParts, contentPart)
		}
	}

	return port.ChatTurn{
		Content:   strings.TrimSpace(content.String()),
		ToolCalls: toolCalls,
		Parts:     contentParts,
	}, nil
}

func toWireTools(tools []port.Tool) []toolGroupWire {
	if len(tools) == 0 {
		return nil
	}

	decls := make([]functionDeclWire, len(tools))
	for i, tool := range tools {
		decls[i] = functionDeclWire{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  normalizeSchema(tool.Parameters),
		}
	}

	return []toolGroupWire{{FunctionDeclarations: decls}}
}

func normalizeSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "OBJECT", "properties": map[string]any{}}
	}

	out := make(map[string]any, len(schema))
	for key, value := range schema {
		switch key {
		case "type":
			if typeName, ok := value.(string); ok {
				out[key] = strings.ToUpper(typeName)
				continue
			}
		case "properties":
			if props, ok := value.(map[string]any); ok {
				normalized := make(map[string]any, len(props))
				for propName, propSchema := range props {
					if propMap, ok := propSchema.(map[string]any); ok {
						normalized[propName] = normalizeSchema(propMap)
					} else {
						normalized[propName] = propSchema
					}
				}
				out[key] = normalized
				continue
			}
		case "items":
			if itemMap, ok := value.(map[string]any); ok {
				out[key] = normalizeSchema(itemMap)
				continue
			}
		}
		out[key] = value
	}

	return out
}

func normalizeModel(model string) string {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(model, "models/") {
		return strings.TrimPrefix(model, "models/")
	}
	return model
}
