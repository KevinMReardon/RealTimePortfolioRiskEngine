package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatMessage is one entry in an OpenAI chat completions request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Err *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// CreateChatCompletion POSTs to baseURL/chat/completions with Bearer apiKey.
// baseURL must be trimmed (e.g. https://api.openai.com/v1). Returns assistant text, HTTP status, and error.
func CreateChatCompletion(ctx context.Context, httpClient *http.Client, apiKey, baseURL, model string, messages []ChatMessage) (content string, httpStatus int, err error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	body, err := json.Marshal(chatCompletionRequest{Model: model, Messages: messages})
	if err != nil {
		return "", 0, err
	}
	url := baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	req.Header.Set("Content-Type", "application/json")

	res, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("openai http: %w", err)
	}
	defer res.Body.Close()
	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", res.StatusCode, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", res.StatusCode, fmt.Errorf("openai: http %d: %s", res.StatusCode, bytes.TrimSpace(respBody))
	}
	var parsed chatCompletionResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", res.StatusCode, fmt.Errorf("openai decode: %w", err)
	}
	if parsed.Err != nil && parsed.Err.Message != "" {
		return "", res.StatusCode, fmt.Errorf("openai api error: %s", parsed.Err.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", res.StatusCode, fmt.Errorf("openai: empty choices")
	}
	text := strings.TrimSpace(parsed.Choices[0].Message.Content)
	if text == "" {
		return "", res.StatusCode, fmt.Errorf("openai: empty message content")
	}
	return text, res.StatusCode, nil
}
