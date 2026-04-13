package events

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// AppendResult is the outcome of appending to the events table. Inserted is false when
// the row already existed for (portfolio_id, idempotency_key); EventID is always the
// durable id (new or existing) so HTTP can respond idempotently.
type AppendResult struct {
	EventID  uuid.UUID
	Inserted bool
}

// PortfolioSnapshotInsertResult is the outcome of InsertPortfolioSnapshot. Inserted is false
// when the same checkpoint was retried and coalesced (unique on portfolio_id + as_of tuple).
type PortfolioSnapshotInsertResult struct {
	Inserted bool
}

// LatestPortfolioSnapshot is the latest checkpoint row for a portfolio (Found false if none).
type LatestPortfolioSnapshot struct {
	Found bool
	AsOf  Cursor
	// CreatedAt is the row created_at (insertion time).
	CreatedAt time.Time
	// SnapshotJSON is the raw JSONB (rtpre_trade_positions_v1 when written by this service).
	SnapshotJSON json.RawMessage
	// Aggregate holds exactly one partition keyed as portfolioID.String() when Found; nil when !Found.
	Aggregate *portfolio.Aggregate
}

// Writer contains mutating event/projection operations.
type Writer interface {
	Append(ctx context.Context, event domain.EventEnvelope) (AppendResult, error)
	LoadPortfolioPositionsIntoAggregate(ctx context.Context, portfolioID uuid.UUID, portfolioKey string, agg *portfolio.Aggregate) error
	// ApplyBatch persists state after the caller has applied each event in applied to agg in order.
	// One transaction: touched trade symbols in positions_projection, then cursor to the last event.
	ApplyBatch(ctx context.Context, portfolioID uuid.UUID, portfolioKey string, applied []domain.EventEnvelope, agg *portfolio.Aggregate) error
	// ApplyPriceBatch updates prices_projection from PriceUpdated events and advances projection_cursor for the price stream partition.
	ApplyPriceBatch(ctx context.Context, streamPortfolioID uuid.UUID, applied []domain.EventEnvelope) error
	PersistApplyDLQ(ctx context.Context, portfolioID uuid.UUID, portfolioKey string, ev domain.EventEnvelope, reason string, cause error) error
	// InsertPortfolioSnapshot appends a portfolio_snapshots row. Duplicate (portfolio_id, as_of_event_time, as_of_event_id) returns Inserted=false.
	InsertPortfolioSnapshot(ctx context.Context, portfolioID uuid.UUID, asOfEventTime time.Time, asOfEventID uuid.UUID, snapshot json.RawMessage) (PortfolioSnapshotInsertResult, error)
}

// Reader contains read-only stream/projection queries.
type Reader interface {
	ListPortfolioIDs(ctx context.Context) ([]uuid.UUID, error)
	// ListPortfolioIDsNotIn returns distinct portfolio_id present in events, excluding any in exclude (e.g. all price stream partitions).
	ListPortfolioIDsNotIn(ctx context.Context, exclude []uuid.UUID) ([]uuid.UUID, error)
	FetchAllForPortfolio(ctx context.Context, portfolioID uuid.UUID) ([]domain.EventEnvelope, error)
	// FetchAfter returns events strictly after the cursor in apply order (no watermark).
	FetchAfter(ctx context.Context, portfolioID uuid.UUID, after Cursor, limit int) ([]domain.EventEnvelope, error)
	// MaxEventTime is max_seen for watermark (MAX(event_time); zero if no events for the partition).
	MaxEventTime(ctx context.Context, portfolioID uuid.UUID) (time.Time, error)
	// LoadProjectionCursor returns the last applied ordering key, or zero if no row (see cursor.go).
	LoadProjectionCursor(ctx context.Context, portfolioID uuid.UUID) (Cursor, error)
	// LoadPortfolioAssemblerInput loads read-model input for GET /v1/portfolios/{id}.
	LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error)
	// ListPortfolioIDsByOpenSymbols returns distinct real portfolio IDs with non-zero quantity for any symbol.
	ListPortfolioIDsByOpenSymbols(ctx context.Context, symbols []string) ([]uuid.UUID, error)
	// LoadSymbolSigma1D computes per-symbol 1-day sigma from the latest windowN returns in symbol_returns.
	LoadSymbolSigma1D(ctx context.Context, symbols []string, windowN int) (map[string]decimal.Decimal, error)
	// LoadSymbolReturnSampleCounts counts non-null daily_return rows per symbol within the latest windowN dates (same window as sigma).
	LoadSymbolReturnSampleCounts(ctx context.Context, symbols []string, windowN int) (map[string]int, error)
	// ListRecentEventsForPortfolio returns the last limit events in chronological order (oldest first).
	ListRecentEventsForPortfolio(ctx context.Context, portfolioID uuid.UUID, limit int) ([]domain.EventEnvelope, error)
	// LoadLatestPortfolioSnapshot returns the newest row by (as_of_event_time, as_of_event_id) and hydrates Aggregate.
	LoadLatestPortfolioSnapshot(ctx context.Context, portfolioID uuid.UUID) (LatestPortfolioSnapshot, error)
	// UpsertRiskSnapshot persists optional materialized risk output.
	UpsertRiskSnapshot(ctx context.Context, portfolioID uuid.UUID, asOfEventTime time.Time, asOfEventID uuid.UUID, snapshot json.RawMessage) error
}

// Repository combines all read/write behavior for current concrete store.
type Repository interface {
	Reader
	Writer
}
