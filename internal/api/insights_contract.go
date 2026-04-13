package api

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// InsightsRecentEventsLimit is the fixed tail size for recent-events context (LLD §11).
const InsightsRecentEventsLimit = 20

// Insights detail.reason values (LLD §12 INSUFFICIENT_DATA sub-reasons).
const (
	InsightsReasonOpenAINotConfigured       = "OPENAI_NOT_CONFIGURED"
	InsightsReasonRiskReadUnavailable       = "INSIGHTS_RISK_READ_UNAVAILABLE"
	InsightsReasonUnpricedOpenPositions     = "UNPRICED_OPEN_POSITIONS"
	InsightsReasonInsufficientReturnHistory = "INSUFFICIENT_RETURN_HISTORY"
)

// InsightsEventSummary is a redacted tail event for AI context (no raw payload, no idempotency keys).
type InsightsEventSummary struct {
	EventID   string         `json:"event_id"`
	EventType string         `json:"event_type"`
	EventTime string         `json:"event_time"`
	Source    string         `json:"source,omitempty"`
	Summary   map[string]any `json:"summary"`
}

// InsightsExplainContext is the structured input bundle for insights (LLD §11).
type InsightsExplainContext struct {
	PortfolioID   string                 `json:"portfolio_id"`
	Portfolio     portfolio.PortfolioView  `json:"portfolio"`
	Risk          RiskHTTPResponse       `json:"risk"`
	RecentEvents  []InsightsEventSummary `json:"recent_events"`
	ClientPayload json.RawMessage        `json:"client_payload,omitempty"`
}

// InsightsExplainRequest is the v1 input to AI explain (path id + optional client JSON + assembled context).
type InsightsExplainRequest struct {
	PortfolioID uuid.UUID
	Payload     json.RawMessage
	Context     *InsightsExplainContext
}

// InsightsExplainResponse is the successful explain payload (LLD §11 audit: used_metrics always present).
type InsightsExplainResponse struct {
	Context *InsightsExplainContext `json:"context,omitempty"`
	// Explanation is the model narrative (plain text or narrative field from structured JSON).
	Explanation string `json:"explanation"`
	// UsedMetrics lists JSON field paths: model-provided when present and validated, else server-derived from context.
	UsedMetrics []string `json:"used_metrics"`
	Model       string   `json:"model"`
}

// InsightsService generates portfolio insights (e.g. OpenAI-backed narrative).
// Nil means the feature is disabled at wiring time (no OPENAI_API_KEY); the HTTP handler
// returns a stable §12 envelope without calling a provider.
type InsightsService interface {
	Explain(ctx context.Context, req InsightsExplainRequest) (*InsightsExplainResponse, error)
}
