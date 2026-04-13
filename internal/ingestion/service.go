package ingestion

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// service is the concrete ingestion implementation that validates domain events
// before appending them to the event store.
type service struct {
	store appender
}

type appender interface {
	Append(ctx context.Context, event domain.EventEnvelope) (events.AppendResult, error)
}

// NewService wires a new ingestion Service around the provided event store.
func NewService(store appender) Service {
	return &service{store: store}
}

// Ingest validates the envelope and its payload, then appends to the store.
func (s *service) Ingest(ctx context.Context, e domain.EventEnvelope) (events.AppendResult, error) {
	if err := domain.ValidateEventEnvelope(e); err != nil {
		return events.AppendResult{}, err
	}

	switch e.EventType {
	case domain.EventTypeTradeExecuted:
		var payload domain.TradePayload
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return events.AppendResult{}, fmt.Errorf("%w: trade payload decode: %v", domain.ErrInvalidPayload, err)
		}
		if err := domain.ValidateTradePayload(payload); err != nil {
			return events.AppendResult{}, err
		}

	case domain.EventTypePriceUpdated:
		var payload domain.PricePayload
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			return events.AppendResult{}, fmt.Errorf("%w: price payload decode: %v", domain.ErrInvalidPayload, err)
		}
		if err := domain.ValidatePricePayload(payload); err != nil {
			return events.AppendResult{}, err
		}

	default:
		// Should already be caught by ValidateEventEnvelope, but keep a hard guard.
		return events.AppendResult{}, fmt.Errorf("%w: unsupported event_type", domain.ErrInvalidEvent)
	}

	return s.store.Append(ctx, e)
}

// Ensure service implements Service.
var _ Service = (*service)(nil)

