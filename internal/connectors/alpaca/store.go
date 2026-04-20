package alpaca

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncState is durable progress for Alpaca activities sync for one portfolio.
// ActivitiesPageToken is Alpaca's page_token when resuming a multi-page pull.
// ActivitiesAfterTime is the optional exclusive lower bound for the next REST "after" filter
// when starting a new sweep (aligned with ListActivitiesRequest.After).
type SyncState struct {
	PortfolioID         uuid.UUID
	LastSuccessAt       *time.Time
	ActivitiesPageToken *string
	ActivitiesAfterTime *time.Time
	LastError           *string
	UpdatedAt           time.Time
}

// SyncStateStore persists SyncState in alpaca_sync_state.
type SyncStateStore struct {
	pool *pgxpool.Pool
}

// NewSyncStateStore returns a store backed by pool.
func NewSyncStateStore(pool *pgxpool.Pool) *SyncStateStore {
	return &SyncStateStore{pool: pool}
}

// Get returns the row for portfolioID, or (nil, nil) when no row exists.
func (s *SyncStateStore) Get(ctx context.Context, portfolioID uuid.UUID) (*SyncState, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("alpaca sync state get: nil store")
	}
	var lastSuccess sql.NullTime
	var afterTime sql.NullTime
	var pageToken, lastErr sql.NullString
	var updatedAt time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT last_success_at, activities_page_token, activities_after_time, last_error, updated_at
		FROM alpaca_sync_state
		WHERE portfolio_id = $1
	`, portfolioID).Scan(
		&lastSuccess,
		&pageToken,
		&afterTime,
		&lastErr,
		&updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("alpaca sync state get: %w", err)
	}

	out := &SyncState{
		PortfolioID: portfolioID,
		UpdatedAt:   updatedAt.UTC(),
	}
	if lastSuccess.Valid {
		t := lastSuccess.Time.UTC()
		out.LastSuccessAt = &t
	}
	if afterTime.Valid {
		t := afterTime.Time.UTC()
		out.ActivitiesAfterTime = &t
	}
	if pageToken.Valid {
		v := pageToken.String
		out.ActivitiesPageToken = &v
	}
	if lastErr.Valid {
		v := lastErr.String
		out.LastError = &v
	}
	return out, nil
}

// Upsert inserts or replaces the sync state row for state.PortfolioID.
func (s *SyncStateStore) Upsert(ctx context.Context, state SyncState) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("alpaca sync state upsert: nil store")
	}
	if state.PortfolioID == uuid.Nil {
		return fmt.Errorf("alpaca sync state upsert: portfolio_id required")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO alpaca_sync_state (
			portfolio_id, last_success_at, activities_page_token, activities_after_time, last_error, updated_at
		) VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (portfolio_id) DO UPDATE SET
			last_success_at = EXCLUDED.last_success_at,
			activities_page_token = EXCLUDED.activities_page_token,
			activities_after_time = EXCLUDED.activities_after_time,
			last_error = EXCLUDED.last_error,
			updated_at = NOW()
	`,
		state.PortfolioID,
		state.LastSuccessAt,
		state.ActivitiesPageToken,
		state.ActivitiesAfterTime,
		state.LastError,
	)
	if err != nil {
		return fmt.Errorf("alpaca sync state upsert: %w", err)
	}
	return nil
}
