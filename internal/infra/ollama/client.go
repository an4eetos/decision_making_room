package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

type Client struct {
	baseURL    string
	chatModel  string
	embedModel string
	httpClient *http.Client
}

func NewClient(baseURL, chatModel, embedModel string) *Client {
	return &Client{
		baseURL:    baseURL,
		chatModel:  chatModel,
		embedModel: embedModel,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
	Tools    []toolWire    `json:"tools,omitempty"`
}

type toolWire struct {
	Type     string       `json:"type"`
	Function toolDefWire  `json:"function"`
}

type toolDefWire struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatMessage struct {
	Role      string          `json:"role"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []toolCallWire  `json:"tool_calls,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
}

type toolCallWire struct {
	Type     string           `json:"type"`
	Function toolFunctionWire `json:"function"`
}

type toolFunctionWire struct {
	Index     int             `json:"index,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
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
	reqMessages, err := toWireMessages(messages)
	if err != nil {
		return port.ChatTurn{}, err
	}

	body, err := json.Marshal(chatRequest{
		Model:    c.chatModel,
		Messages: reqMessages,
		Stream:   false,
		Tools:    toWireTools(tools),
	})
	if err != nil {
		return port.ChatTurn{}, fmt.Errorf("marshal chat request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return port.ChatTurn{}, fmt.Errorf("create chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return port.ChatTurn{}, fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return port.ChatTurn{}, fmt.Errorf("read chat response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return port.ChatTurn{}, fmt.Errorf("chat failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result chatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return port.ChatTurn{}, fmt.Errorf("unmarshal chat response: %w", err)
	}

	toolCalls, err := parseToolCalls(result.Message.ToolCalls)
	if err != nil {
		return port.ChatTurn{}, err
	}

	return port.ChatTurn{
		Content:   result.Message.Content,
		ToolCalls: toolCalls,
	}, nil
}

func toWireMessages(messages []port.Message) ([]chatMessage, error) {
	out := make([]chatMessage, len(messages))
	for i, m := range messages {
		wire := chatMessage{
			Role:     m.Role,
			Content:  m.Content,
			ToolName: m.ToolName,
		}
		if len(m.ToolCalls) > 0 {
			calls, err := toWireToolCalls(m.ToolCalls)
			if err != nil {
				return nil, err
			}
			wire.ToolCalls = calls
		}
		out[i] = wire
	}
	return out, nil
}

func toWireToolCalls(calls []port.ToolCall) ([]toolCallWire, error) {
	out := make([]toolCallWire, len(calls))
	for i, call := range calls {
		argsJSON, err := json.Marshal(call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("marshal tool arguments: %w", err)
		}
		out[i] = toolCallWire{
			Type: "function",
			Function: toolFunctionWire{
				Index:     i,
				Name:      call.Name,
				Arguments: argsJSON,
			},
		}
	}
	return out, nil
}

func parseToolCalls(calls []toolCallWire) ([]port.ToolCall, error) {
	out := make([]port.ToolCall, 0, len(calls))
	for _, call := range calls {
		args, err := parseArguments(call.Function.Arguments)
		if err != nil {
			return nil, err
		}
		out = append(out, port.ToolCall{
			Name:      call.Function.Name,
			Arguments: args,
		})
	}
	return out, nil
}

func parseArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}

	var args map[string]any
	if err := json.Unmarshal(raw, &args); err == nil {
		return args, nil
	}

	var argsStr string
	if err := json.Unmarshal(raw, &argsStr); err != nil {
		return nil, fmt.Errorf("parse tool arguments: %w", err)
	}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return nil, fmt.Errorf("parse tool arguments string: %w", err)
	}
	return args, nil
}

func toWireTools(tools []port.Tool) []toolWire {
	if len(tools) == 0 {
		return nil
	}
	out := make([]toolWire, len(tools))
	for i, tool := range tools {
		out[i] = toolWire{
			Type: "function",
			Function: toolDefWire{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		}
	}
	return out
}

type embedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type embedResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (c *Client) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{
		Model:  c.embedModel,
		Prompt: text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create embed request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read embed response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result embedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal embed response: %w", err)
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("empty embedding returned")
	}

	return result.Embedding, nil
}
