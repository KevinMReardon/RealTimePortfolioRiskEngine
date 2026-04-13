package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EventType is the canonical event_type string on the envelope (LLD §4.1).
type EventType string

const (
	// EventTypeTradeExecuted indicates payload follows LLD §4.2 (TradeExecuted).
	EventTypeTradeExecuted EventType = "TradeExecuted"
	// EventTypePriceUpdated indicates payload follows LLD §4.3 (PriceUpdated).
	EventTypePriceUpdated EventType = "PriceUpdated"
)

// EventEnvelope is the canonical ingestion and event-store record (LLD §4.1).
// JSON field names match the LLD; Payload is type-specific JSON for the EventType.
type EventEnvelope struct {
	EventID        uuid.UUID       `json:"event_id"`
	EventType      EventType       `json:"event_type"`
	EventTime      time.Time       `json:"event_time"`
	ProcessingTime time.Time       `json:"processing_time"`
	Source         string          `json:"source"`
	PortfolioID    string          `json:"portfolio_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
}
