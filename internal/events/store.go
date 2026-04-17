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

// PortfolioCatalogEntry is a user-facing portfolio directory row.
type PortfolioCatalogEntry struct {
	PortfolioID  uuid.UUID
	OwnerUserID  uuid.UUID
	Name         string
	BaseCurrency string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserAccount struct {
	UserID       uuid.UUID
	DisplayName  string
	WorkEmail    string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserSession struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
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
	// ListPortfolios returns user-facing portfolio catalog rows sorted by recency.
	ListPortfolios(ctx context.Context) ([]PortfolioCatalogEntry, error)
	// ListPortfoliosByOwner returns catalog rows for exactly one owner.
	ListPortfoliosByOwner(ctx context.Context, ownerUserID uuid.UUID) ([]PortfolioCatalogEntry, error)
	// CreatePortfolio inserts one portfolio catalog row.
	CreatePortfolio(ctx context.Context, portfolioID uuid.UUID, name, baseCurrency string) (PortfolioCatalogEntry, error)
	// CreatePortfolioForOwner inserts one owner-scoped portfolio catalog row.
	CreatePortfolioForOwner(ctx context.Context, ownerUserID, portfolioID uuid.UUID, name, baseCurrency string) (PortfolioCatalogEntry, error)
	// PortfolioOwnedByUser reports whether a portfolio belongs to a specific user.
	PortfolioOwnedByUser(ctx context.Context, portfolioID, ownerUserID uuid.UUID) (bool, error)
	// CreateUser inserts one user account row.
	CreateUser(ctx context.Context, user UserAccount) (UserAccount, error)
	// GetUserByEmail finds one user by work email (case-insensitive).
	GetUserByEmail(ctx context.Context, workEmail string) (UserAccount, bool, error)
	// GetUserByID finds one user by user id.
	GetUserByID(ctx context.Context, userID uuid.UUID) (UserAccount, bool, error)
	// CreateSession inserts one user session row.
	CreateSession(ctx context.Context, session UserSession) (UserSession, error)
	// GetSessionByID returns one session row when active and present.
	GetSessionByID(ctx context.Context, sessionID uuid.UUID) (UserSession, bool, error)
	// RevokeSession marks one session as revoked.
	RevokeSession(ctx context.Context, sessionID uuid.UUID) error
	// LoadPriceFeedWatchlist returns the persisted automated feed watchlist.
	// found=false indicates no persisted value is present.
	LoadPriceFeedWatchlist(ctx context.Context) (watchlist []string, found bool, err error)
	// UpsertPriceFeedWatchlist persists the automated feed watchlist.
	UpsertPriceFeedWatchlist(ctx context.Context, watchlist []string) error
}

// Repository combines all read/write behavior for current concrete store.
type Repository interface {
	Reader
	Writer
}
