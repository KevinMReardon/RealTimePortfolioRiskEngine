package api

import (
	"encoding/json"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// eventSummaryForInsights maps a stored envelope to a redacted summary (symbol hints only; no secrets).
func eventSummaryForInsights(ev domain.EventEnvelope) InsightsEventSummary {
	out := InsightsEventSummary{
		EventID:   ev.EventID.String(),
		EventType: string(ev.EventType),
		EventTime: ev.EventTime.UTC().Format(time.RFC3339Nano),
		Source:    ev.Source,
		Summary:   map[string]any{},
	}
	switch ev.EventType {
	case domain.EventTypeTradeExecuted:
		var p domain.TradePayload
		if err := json.Unmarshal(ev.Payload, &p); err == nil {
			if p.Symbol != "" {
				out.Summary["symbol"] = p.Symbol
			}
			if p.Side != "" {
				out.Summary["side"] = string(p.Side)
			}
		}
	case domain.EventTypePriceUpdated:
		var p domain.PricePayload
		if err := json.Unmarshal(ev.Payload, &p); err == nil && p.Symbol != "" {
			out.Summary["symbol"] = p.Symbol
		}
	default:
		out.Summary["event_type"] = string(ev.EventType)
	}
	return out
}
