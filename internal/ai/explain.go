package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// ExplainInput is a transport-agnostic bundle for the insights explain pipeline.
type ExplainInput struct {
	ContextJSON   []byte
	ClientPayload []byte
	AllowSymbols  map[string]struct{}
	Model         string
	BaseURL       string
}

// ExplainOutput is the validated model result (before merging with HTTP response envelope).
type ExplainOutput struct {
	Explanation string
	UsedMetrics []string
	Model       string
}

// Explain builds chat messages, calls CreateChatCompletion, parses assistant JSON when present,
// runs ValidateInsightOutput, and returns narrative + used_metrics.
func Explain(ctx context.Context, httpClient *http.Client, apiKey string, in ExplainInput) (*ExplainOutput, error) {
	if len(in.ContextJSON) == 0 {
		return nil, fmt.Errorf("ai explain: missing context JSON")
	}
	if in.AllowSymbols == nil {
		in.AllowSymbols = map[string]struct{}{}
	}
	userFacts, err := BuildUserContent(in.ContextJSON)
	if err != nil {
		return nil, err
	}
	payload := strings.TrimSpace(string(in.ClientPayload))
	if payload == "" {
		payload = "{}"
	}
	user := fmt.Sprintf(
		"OPTIONAL_CLIENT_REQUEST (non-authoritative; do not invent facts from unknown keys):\n%s\n\nCONTEXT:\n%s",
		payload,
		userFacts,
	)
	text, _, err := CreateChatCompletion(ctx, httpClient, apiKey, in.BaseURL, in.Model, []ChatMessage{
		{Role: "system", Content: ExplainSystemPrompt},
		{Role: "user", Content: user},
	})
	if err != nil {
		return nil, err
	}
	text = stripMarkdownCodeFence(text)
	var structured struct {
		Narrative   string   `json:"narrative"`
		UsedMetrics []string `json:"used_metrics"`
	}
	structuredOK := json.Unmarshal([]byte(text), &structured) == nil &&
		(structured.Narrative != "" || len(structured.UsedMetrics) > 0)
	usedMetrics := mergeUsedMetrics(structured.UsedMetrics, in.ContextJSON)
	if err := ValidateInsightOutput(in.AllowSymbols, text, usedMetrics); err != nil {
		var valErr *ValidationError
		if errors.As(err, &valErr) && valErr.Code == ValReasonUnknownSymbol {
			sanitized := SanitizeUnknownTickerTokens(text, in.AllowSymbols)
			if sanitized != text {
				if sanitizeErr := ValidateInsightOutput(in.AllowSymbols, sanitized, usedMetrics); sanitizeErr == nil {
					text = sanitized
					structured = struct {
						Narrative   string   `json:"narrative"`
						UsedMetrics []string `json:"used_metrics"`
					}{}
					structuredOK = json.Unmarshal([]byte(text), &structured) == nil &&
						(structured.Narrative != "" || len(structured.UsedMetrics) > 0)
				}
			}
		}
		if err := ValidateInsightOutput(in.AllowSymbols, text, usedMetrics); err != nil {
			return nil, err
		}
	}
	if structuredOK {
		return &ExplainOutput{
			Explanation: structured.Narrative,
			UsedMetrics: usedMetrics,
			Model:       in.Model,
		}, nil
	}
	return &ExplainOutput{Explanation: text, UsedMetrics: usedMetrics, Model: in.Model}, nil
}

// mergeUsedMetrics prefers non-empty model-provided paths (deduped); otherwise server-fills
// from context JSON for LLD §11 auditability (including non-JSON model replies).
func mergeUsedMetrics(modelPaths []string, contextJSON []byte) []string {
	out := make([]string, 0, len(modelPaths))
	seen := make(map[string]struct{})
	for _, p := range modelPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if len(out) > 0 {
		return out
	}
	return DefaultUsedMetricsFromContextJSON(contextJSON)
}

func stripMarkdownCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimPrefix(s, "json")
		s = strings.TrimSpace(s)
		if idx := strings.LastIndex(s, "```"); idx >= 0 {
			s = strings.TrimSpace(s[:idx])
		}
	}
	return strings.TrimSpace(s)
}
