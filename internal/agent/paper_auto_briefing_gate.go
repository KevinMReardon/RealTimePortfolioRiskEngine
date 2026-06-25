package agent

import (
	"context"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// PaperAutoBriefingGate supports scoped paper-auto retries tied to briefing sessions.
type PaperAutoBriefingGate interface {
	GetLatestSucceededAgentSessionForPortfolio(ctx context.Context, portfolioID uuid.UUID) (events.AgentSession, bool, error)
	PortfolioHasActiveBriefing(ctx context.Context, portfolioID uuid.UUID) (bool, error)
}
