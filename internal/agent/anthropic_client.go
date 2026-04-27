package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicClient abstracts the provider transport layer.
type AnthropicClient interface {
	CreateMessage(ctx context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error)
}

type AnthropicMessageRequest struct {
	Model       string              `json:"model"`
	System      string              `json:"system"`
	Temperature *float64            `json:"temperature,omitempty"`
	MaxTokens   *int                `json:"max_tokens,omitempty"`
	Messages    []AnthropicMessage  `json:"messages"`
	Tools       []AnthropicToolSpec `json:"tools,omitempty"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

type AnthropicToolSpec struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"input_schema"`
}

type AnthropicToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input any    `json:"input"`
}

type AnthropicMessageResponse struct {
	ProviderID   string              `json:"id"`
	StopReason   string              `json:"stop_reason"`
	OutputText   string              `json:"output_text"`
	ToolCalls    []AnthropicToolCall `json:"tool_calls,omitempty"`
	// AssistantMessage preserves provider-returned assistant content blocks.
	// It is used to replay the exact prior assistant tool_use message before tool_result.
	AssistantMessage AnthropicMessage `json:"assistant_message,omitempty"`
	InputTokens  *int                `json:"input_tokens,omitempty"`
	OutputTokens *int                `json:"output_tokens,omitempty"`
	Raw          []byte              `json:"raw,omitempty"`
}

type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

type HTTPAnthropicClient struct {
	apiKey         string
	baseURL        string
	version        string
	httpClient     *http.Client
	maxRetries     int
	requestTimeout time.Duration
}

func NewHTTPAnthropicClient(apiKey, baseURL string) *HTTPAnthropicClient {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = "https://api.anthropic.com"
	}
	base = strings.TrimRight(base, "/")
	return &HTTPAnthropicClient{
		apiKey:         strings.TrimSpace(apiKey),
		baseURL:        base,
		version:        "2023-06-01",
		httpClient:     &http.Client{},
		maxRetries:     3,
		requestTimeout: 0,
	}
}

type anthropicMessagesAPIResponse struct {
	ID         string `json:"id"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *HTTPAnthropicClient) CreateMessage(ctx context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	if c == nil {
		return AnthropicMessageResponse{}, fmt.Errorf("anthropic client: nil receiver")
	}
	if c.apiKey == "" {
		return AnthropicMessageResponse{}, fmt.Errorf("anthropic client: missing api key")
	}
	if strings.TrimSpace(req.Model) == "" {
		return AnthropicMessageResponse{}, fmt.Errorf("anthropic client: model is required")
	}
	if len(req.Messages) == 0 {
		return AnthropicMessageResponse{}, fmt.Errorf("anthropic client: at least one message is required")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return AnthropicMessageResponse{}, fmt.Errorf("anthropic encode request: %w", err)
	}
	var lastErr error
	backoff := 200 * time.Millisecond
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		reqCtx := ctx
		var cancel context.CancelFunc
		if c.requestTimeout > 0 {
			reqCtx, cancel = context.WithTimeout(ctx, c.requestTimeout)
		}
		httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
		if err != nil {
			if cancel != nil {
				cancel()
			}
			return AnthropicMessageResponse{}, fmt.Errorf("anthropic new request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("x-api-key", c.apiKey)
		httpReq.Header.Set("anthropic-version", c.version)

		res, err := c.httpClient.Do(httpReq)
		if cancel != nil {
			cancel()
		}
		if err != nil {
			lastErr = fmt.Errorf("anthropic http do: %w", err)
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return AnthropicMessageResponse{}, lastErr
			}
			if attempt < c.maxRetries {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return AnthropicMessageResponse{}, lastErr
		}
		respBody, readErr := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if readErr != nil {
			return AnthropicMessageResponse{}, fmt.Errorf("anthropic read response: %w", readErr)
		}

		retryable := res.StatusCode == http.StatusTooManyRequests || (res.StatusCode >= 500 && res.StatusCode <= 599)
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			lastErr = fmt.Errorf("anthropic http %d: %s", res.StatusCode, strings.TrimSpace(string(respBody)))
			if retryable && attempt < c.maxRetries {
				time.Sleep(backoff)
				backoff *= 2
				continue
			}
			return AnthropicMessageResponse{}, lastErr
		}

		var parsed anthropicMessagesAPIResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return AnthropicMessageResponse{}, fmt.Errorf("anthropic decode response: %w", err)
		}
		if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
			return AnthropicMessageResponse{}, fmt.Errorf("anthropic api error: %s", parsed.Error.Message)
		}
		out := AnthropicMessageResponse{
			ProviderID: parsed.ID,
			StopReason: strings.TrimSpace(parsed.StopReason),
			ToolCalls:  make([]AnthropicToolCall, 0),
			AssistantMessage: AnthropicMessage{
				Role:    "assistant",
				Content: make([]AnthropicContentBlock, 0, len(parsed.Content)),
			},
			Raw:        append([]byte(nil), respBody...),
		}
		if parsed.Usage.InputTokens > 0 {
			v := parsed.Usage.InputTokens
			out.InputTokens = &v
		}
		if parsed.Usage.OutputTokens > 0 {
			v := parsed.Usage.OutputTokens
			out.OutputTokens = &v
		}
		var textParts []string
		for _, block := range parsed.Content {
			out.AssistantMessage.Content = append(out.AssistantMessage.Content, AnthropicContentBlock{
				Type:      block.Type,
				Text:      block.Text,
				ID:        block.ID,
				Name:      block.Name,
				Input:     append(json.RawMessage(nil), block.Input...),
				ToolUseID: block.ToolUseID,
			})
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					textParts = append(textParts, block.Text)
				}
			case "tool_use":
				out.ToolCalls = append(out.ToolCalls, AnthropicToolCall{
					ID:    block.ID,
					Name:  block.Name,
					Input: json.RawMessage(block.Input),
				})
			}
		}
		out.OutputText = strings.TrimSpace(strings.Join(textParts, "\n"))
		return out, nil
	}
	if lastErr != nil {
		return AnthropicMessageResponse{}, lastErr
	}
	return AnthropicMessageResponse{}, fmt.Errorf("anthropic client: request failed")
}
