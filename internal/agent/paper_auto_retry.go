package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals/submit"
)

// PaperAutoRetryConfig controls periodic retry of recent proposals.
type PaperAutoRetryConfig struct {
	Enabled    bool
	Interval   time.Duration
	Lookback   time.Duration
	MaxPerTick int
}

// PaperAutoRetryRunner revisits recent proposed/approved rows and attempts submit again
// when market conditions have changed (for example market-hours reopen).
type PaperAutoRetryRunner struct {
	Config  PaperAutoRetryConfig
	Auto    *PaperAutoRunner
	Catalog BriefingEligibilityLister
	Log     *zap.Logger
}

func (r *PaperAutoRetryRunner) Run(ctx context.Context) {
	if r == nil || !r.Config.Enabled || r.Auto == nil || r.Catalog == nil {
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
	r.tick(ctx, log)
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
	if !policy.IsUSRegularSessionEquities(time.Now()) {
		return
	}
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
	keys, linked, err := auto.Keys.LoadPortfolioAlpacaKeyMaterial(ctx, portfolioID)
	if err != nil || !linked || !submit.IsPaperLinked(keys) {
		return
	}
	lookback := r.Config.Lookback
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}
	max := r.Config.MaxPerTick
	if max <= 0 {
		max = 10
	}
	since := time.Now().UTC().Add(-lookback)
	submitted := 0
	for _, status := range []string{"proposed", "approved"} {
		if submitted >= max || ctx.Err() != nil {
			return
		}
		st := status
		list, err := auto.Store.ListByPortfolio(ctx, portfolioID, proposals.ListFilter{Status: &st})
		if err != nil {
			log.Warn("paper_auto_retry_list_failed",
				zap.String("portfolio_id", portfolioID.String()),
				zap.String("status", status),
				zap.Error(err),
			)
			continue
		}
		for _, prop := range list {
			if submitted >= max || ctx.Err() != nil {
				return
			}
			if prop.CreatedAt.Before(since) {
				continue
			}
			switch status {
			case "proposed":
				if auto.processOneWithOptions(ctx, log, portfolioID, prop.ProposalID, true) {
					submitted++
				}
			case "approved":
				res := submit.FromProposal(ctx, auto.Submit, prop, submit.Options{})
				if res.Outcome == submit.OutcomeSuccess {
					submitted++
					log.Info("paper_auto_retry_submitted",
						zap.String("portfolio_id", portfolioID.String()),
						zap.String("proposal_id", prop.ProposalID.String()),
						zap.String("broker_order_id", res.BrokerOrderID),
					)
				}
			}
		}
	}
}
