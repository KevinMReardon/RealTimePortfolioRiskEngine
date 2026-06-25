package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
)

// PaperAutoRetryConfig controls periodic retry of proposals from the latest succeeded briefing.
type PaperAutoRetryConfig struct {
	Enabled    bool
	Interval   time.Duration
	MaxPerTick int
	MaxRetries int
}

// PaperAutoRetryRunner revisits proposed/approved rows tied to the latest succeeded session.
type PaperAutoRetryRunner struct {
	Config  PaperAutoRetryConfig
	Auto    *PaperAutoRunner
	Gate    PaperAutoBriefingGate
	Catalog BriefingEligibilityLister
	Log     *zap.Logger
}

func (r *PaperAutoRetryRunner) maxRetries() int {
	if r == nil || r.Config.MaxRetries <= 0 {
		return defaultPaperAutoMaxRetries
	}
	return r.Config.MaxRetries
}

func (r *PaperAutoRetryRunner) Run(ctx context.Context) {
	if r == nil || !r.Config.Enabled || r.Auto == nil || r.Catalog == nil || r.Gate == nil {
		return
	}
	log := r.Log
	if log == nil {
		log = zap.NewNop()
	}
	interval := r.Config.Interval
	if interval <= 0 {
		interval = 3 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx, log)
		}
	}
}

func (r *PaperAutoRetryRunner) tick(ctx context.Context, log *zap.Logger) {
	rows, err := r.Catalog.ListAgentBriefingEligiblePortfolios(ctx)
	if err != nil {
		log.Warn("paper_auto_retry_list_targets_failed", zap.Error(err))
		return
	}
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		r.retryPortfolio(ctx, log, row.PortfolioID)
	}
}

func (r *PaperAutoRetryRunner) retryPortfolio(ctx context.Context, log *zap.Logger, portfolioID uuid.UUID) {
	auto := r.Auto
	if auto == nil || auto.Store == nil || auto.Keys == nil {
		return
	}
	active, err := r.Gate.PortfolioHasActiveBriefing(ctx, portfolioID)
	if err != nil {
		log.Warn("paper_auto_retry_active_check_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.Error(err),
		)
		return
	}
	if active {
		log.Debug("paper_auto_retry_paused",
			zap.String("reason", "briefing_in_flight"),
			zap.String("portfolio_id", portfolioID.String()),
		)
		return
	}
	session, ok, err := r.Gate.GetLatestSucceededAgentSessionForPortfolio(ctx, portfolioID)
	if err != nil {
		log.Warn("paper_auto_retry_session_lookup_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.Error(err),
		)
		return
	}
	if !ok {
		return
	}
	if !auto.paperKeysReady(ctx, log, portfolioID) {
		return
	}
	list, err := auto.Store.ListByAgentSession(ctx, portfolioID, session.SessionID, proposals.ListByAgentSessionFilter{})
	if err != nil {
		log.Warn("paper_auto_retry_list_session_failed",
			zap.String("portfolio_id", portfolioID.String()),
			zap.String("session_id", session.SessionID.String()),
			zap.Error(err),
		)
		return
	}
	max := r.Config.MaxPerTick
	if max <= 0 {
		max = 10
	}
	processed := 0
	for _, prop := range list {
		if processed >= max || ctx.Err() != nil {
			return
		}
		if isPaperAutoTerminalStatus(prop.Status) {
			continue
		}
		if prop.PaperAutoRetryCount >= r.maxRetries() {
			continue
		}
		switch prop.Status {
		case proposals.StatusProposed:
			if auto.processOneRetry(ctx, log, portfolioID, prop.ProposalID) {
				processed++
			} else {
				// Count toward per-tick cap even on failure to avoid hammering one row.
				processed++
			}
		case proposals.StatusApproved:
			if auto.retrySubmitApproved(ctx, log, portfolioID, prop) {
				processed++
			} else {
				processed++
			}
		}
	}
}
