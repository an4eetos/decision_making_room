package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

const (
	defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"
	maxRateRetries = 1
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
	SystemInstruction *contentWire `json:"systemInstruction,omitempty"`
	Contents          []contentWire `json:"contents"`
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

func (c *Client) chat(ctx context.Context, messages []port.Message, tools []port.Tool) (port.ChatTurn, error) {
	systemInstruction, contents, err := toWireContents(messages)
	if err != nil {
		return port.ChatTurn{}, err
	}

	body, err := json.Marshal(generateRequest{
		SystemInstruction: systemInstruction,
		Contents:          contents,
		Tools:             toWireTools(tools),
	})
	if err != nil {
		return port.ChatTurn{}, fmt.Errorf("marshal generate request: %w", err)
	}

	var respBody []byte
	var errOut error
	models := c.chatModels
	if len(models) == 0 {
		models = []string{"gemini-2.0-flash"}
	}
	for _, model := range models {
		url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, normalizeModel(model), c.apiKey)
		respBody, errOut = c.post(ctx, url, body, false)
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

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{
		Content: contentWire{
			Parts: []partWire{{Text: text}},
		},
		OutputDimensionality: 768,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", c.baseURL, normalizeModel(c.embedModel), c.apiKey)
	respBody, err := c.post(ctx, url, body, true)
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

func (c *Client) post(ctx context.Context, url string, body []byte, allowRetry bool) ([]byte, error) {
	var lastBody []byte

	for attempt := 0; attempt <= maxRateRetries; attempt++ {
		respBody, status, err := c.doPost(ctx, url, body)
		if err != nil {
			return nil, err
		}

		if status == http.StatusOK {
			return respBody, nil
		}

		lastBody = respBody
		if !allowRetry || status != http.StatusTooManyRequests || attempt == maxRateRetries {
			break
		}

		delay := parseRetryDelay(respBody)
		if delay <= 0 {
			delay = time.Duration(attempt+1) * 10 * time.Second
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	return nil, fmt.Errorf("request failed: %s", formatAPIError(lastBody))
}

func (c *Client) doPost(ctx context.Context, url string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

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

func isRetryableModelError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "status 429"),
		strings.Contains(message, "resource_exhausted"),
		strings.Contains(message, "high demand"),
		strings.Contains(message, "overloaded"),
		strings.Contains(message, "temporarily unavailable"),
		strings.Contains(message, "status 500"),
		strings.Contains(message, "status 503"):
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
