// Package llm is a minimal OpenAI-compatible chat client: streaming chat
// completions with tool calls, plus model discovery. It speaks the subset of
// the wire format that Ollama Cloud's /v1 endpoint implements.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Message is one chat message. Tool results carry a ToolCallID; assistant
// messages that requested tools carry ToolCalls.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a model request to run a named function with JSON arguments.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name and raw JSON arguments of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Tool describes a function the model may call.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction is the JSON-schema definition of a tool.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// Client talks to an OpenAI-compatible endpoint.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds a client. baseURL is typically https://ollama.com/v1.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

// ListModels returns the sorted model identifiers the endpoint advertises.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.authorize(request)
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("cannot reach %s: %w", c.host(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("model list failed (HTTP %d)%s", response.StatusCode, detailSuffix(response.Body))
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	models := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		if _, ok := seen[model.ID]; ok {
			continue
		}
		seen[model.ID] = struct{}{}
		models = append(models, model.ID)
	}
	sort.Strings(models)
	return models, nil
}

// StreamChat streams one assistant turn. onText receives each text delta as it
// arrives. It returns the assembled assistant message, whose ToolCalls the
// caller should execute before the next turn.
func (c *Client) StreamChat(
	ctx context.Context,
	model string,
	messages []Message,
	tools []Tool,
	onText func(string),
) (Message, error) {
	payload := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Message{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request)

	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
		return Message{}, fmt.Errorf("cannot reach %s: %w", c.host(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Message{}, fmt.Errorf("request failed (HTTP %d)%s", response.StatusCode, detailSuffix(response.Body))
	}

	assembled := Message{Role: "assistant"}
	var text strings.Builder
	toolCalls := map[int]*ToolCall{}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				text.WriteString(choice.Delta.Content)
				if onText != nil {
					onText(choice.Delta.Content)
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := toolCalls[delta.Index]
				if call == nil {
					call = &ToolCall{Type: "function"}
					toolCalls[delta.Index] = call
				}
				if delta.ID != "" {
					call.ID = delta.ID
				}
				if delta.Function.Name != "" {
					call.Function.Name = delta.Function.Name
				}
				call.Function.Arguments += delta.Function.Arguments
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
		return Message{}, err
	}
	if ctx.Err() != nil {
		return Message{}, ctx.Err()
	}

	assembled.Content = text.String()
	indexes := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		assembled.ToolCalls = append(assembled.ToolCalls, *toolCalls[index])
	}
	return assembled, nil
}

func (c *Client) authorize(request *http.Request) {
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
}

// host names the endpoint in error messages, so a failure says what could not be
// reached instead of leaving the reader to guess.
func (c *Client) host() string {
	if parsed, err := url.Parse(c.baseURL); err == nil && parsed.Host != "" {
		return parsed.Host
	}
	return c.baseURL
}

// detailSuffix reads the reason the server gave for rejecting a request. The
// body is written by the endpoint, so it is reduced to a single bounded line
// before it can reach a terminal.
func detailSuffix(body io.Reader) string {
	if reason := errorReason(body); reason != "" {
		return ": " + reason
	}
	return ""
}

// errorReason extracts a short human-readable reason from an error body.
// Endpoints send {"error":{"message":"..."}}, an OpenAI-style envelope with a
// bare string, or plain prose.
func errorReason(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 8<<10))
	text := strings.Join(strings.Fields(string(data)), " ")
	if strings.HasPrefix(text, "{") {
		var envelope struct {
			Error   json.RawMessage `json:"error"`
			Message string          `json:"message"`
		}
		if err := json.Unmarshal([]byte(text), &envelope); err == nil {
			if reason := nestedMessage(envelope.Error); reason != "" {
				text = reason
			} else if envelope.Message != "" {
				text = envelope.Message
			}
		}
	}
	if runes := []rune(text); len(runes) > 300 {
		text = string(runes[:300]) + "…"
	}
	return text
}

// nestedMessage reads the message out of an error field that is either an object
// or a bare string.
func nestedMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var object struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &object); err == nil && object.Message != "" {
		return object.Message
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return ""
}

// Ping sends the smallest possible chat request. Listing models does not
// authenticate -- Ollama Cloud answers /models without a key -- so a real
// completion is the only way to tell an unusable key from a healthy connection.
// It exists for gochat --check.
func (c *Client) Ping(ctx context.Context, model string) error {
	payload := map[string]any{
		"model":      model,
		"messages":   []Message{{Role: "user", Content: "ping"}},
		"max_tokens": 1,
		"stream":     false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	c.authorize(request)

	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("cannot reach %s: %w", c.host(), err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("chat completion failed (HTTP %d)%s", response.StatusCode, detailSuffix(response.Body))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	return nil
}

// ErrNoBody reports a stream that ended without any usable payload.
var ErrNoBody = errors.New("empty response")

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
