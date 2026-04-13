package insights

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ai"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/api"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultOpenAIModel   = "gpt-4o-mini"
)

// OpenAIService implements api.InsightsService using the ai.Explain orchestrator.
type OpenAIService struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	model      string
}

// NewOpenAIService returns a configured client. apiKey must be non-empty (callers should gate on config).
func NewOpenAIService(apiKey, baseURL, model string) *OpenAIService {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultOpenAIModel
	}
	return &OpenAIService{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		apiKey:     strings.TrimSpace(apiKey),
		baseURL:    baseURL,
		model:      model,
	}
}

// Explain delegates to ai.Explain (messages → HTTP → parse → validate).
func (s *OpenAIService) Explain(ctx context.Context, req api.InsightsExplainRequest) (*api.InsightsExplainResponse, error) {
	if s.apiKey == "" {
		return nil, fmt.Errorf("openai: missing api key")
	}
	if req.Context == nil {
		return nil, fmt.Errorf("openai: missing context")
	}
	ctxBytes, err := json.Marshal(req.Context)
	if err != nil {
		return nil, err
	}
	out, err := ai.Explain(ctx, s.httpClient, s.apiKey, ai.ExplainInput{
		ContextJSON:   ctxBytes,
		ClientPayload: req.Payload,
		AllowSymbols:  api.SymbolAllowlistFromInsightsContext(req.Context),
		Model:         s.model,
		BaseURL:       s.baseURL,
	})
	if err != nil {
		return nil, err
	}
	return &api.InsightsExplainResponse{
		Explanation: out.Explanation,
		UsedMetrics: out.UsedMetrics,
		Model:       out.Model,
	}, nil
}
