package ingestion

import (
	"context"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// Service defines ingestion boundary for canonical events.
type Service interface {
	Ingest(ctx context.Context, event domain.EventEnvelope) (events.AppendResult, error)
}
