package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// EquityAnchorEnsurer backfills today's NY calendar equity anchor when missing.
// Implemented by runtime.EquityAnchorJob in production.
type EquityAnchorEnsurer interface {
	EnsureTodayForPortfolioKeys(ctx context.Context, portfolioID uuid.UUID, keys events.PortfolioAlpacaKeyMaterial)
}

// todayAnchorDateUTC is the stored anchor_date for the NY calendar day containing now.
func todayAnchorDateUTC(now time.Time) time.Time {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		loc = time.FixedZone("America/New_York", -5*3600)
	}
	nowNY := now.In(loc)
	return time.Date(nowNY.Year(), nowNY.Month(), nowNY.Day(), 0, 0, 0, 0, time.UTC)
}
