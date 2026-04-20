package events

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ResolveAlpacaPortfolioForSingleUser returns the sole row in portfolios when exactly one catalog
// portfolio exists. Used in single-user paths where the caller needs a deterministic portfolio id
// without additional user input.
func ResolveAlpacaPortfolioForSingleUser(ctx context.Context, pool *pgxpool.Pool) (uuid.UUID, error) {
	rows, err := pool.Query(ctx, `
		SELECT portfolio_id FROM portfolios ORDER BY created_at ASC, portfolio_id ASC
	`)
	if err != nil {
		return uuid.Nil, fmt.Errorf("list portfolios for alpaca resolve: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return uuid.Nil, fmt.Errorf("scan portfolio_id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return uuid.Nil, err
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, fmt.Errorf("no portfolio in catalog: create one via POST /v1/portfolios before Alpaca fill sync")
	case 1:
		return ids[0], nil
	default:
		return uuid.Nil, fmt.Errorf("multiple portfolios in catalog (%d): pass an explicit portfolio id or keep a single-book catalog", len(ids))
	}
}
