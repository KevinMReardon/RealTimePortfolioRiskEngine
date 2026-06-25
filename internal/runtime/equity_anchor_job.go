// Package runtime equity-anchor job: writes one portfolio_equity_anchor row per Alpaca-linked
// portfolio per NY calendar day. Without this row the policy "max daily loss %" rule fails closed
// because EquityAnchor is zero.
package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// EquityAnchorTargetLister enumerates Alpaca-linked portfolios that should receive a daily anchor.
type EquityAnchorTargetLister interface {
	ListAlpacaSyncTargets(ctx context.Context) ([]events.AlpacaSyncTarget, error)
}

// EquityAnchorWriter persists today's start-of-day equity for a portfolio (proposals.Store).
type EquityAnchorWriter interface {
	UpsertEquityAnchor(ctx context.Context, portfolioID uuid.UUID, anchorDate time.Time, equity decimal.Decimal) error
}

var equityAnchorBootRetryDelays = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
}

// EquityAnchorRESTFactory builds an alpaca.REST client from key material. Matches alpaca.NewREST shape.
type EquityAnchorRESTFactory func(cfg alpaca.RESTConfig) (alpaca.REST, error)

// EquityAnchorJob captures today's broker equity for each linked portfolio shortly after NY market open.
//
// Run scheduling: cron expression "30 9 * * 1-5" (9:30 AM Mon-Fri) in America/New_York. Configurable.
// The Tick method is safe to call directly for tests / boot-time backfill.
type EquityAnchorJob struct {
	Targets EquityAnchorTargetLister
	Anchor  EquityAnchorWriter
	NewREST EquityAnchorRESTFactory
	Log     *zap.Logger
	// Cron expression in TZ. Defaults to "30 9 * * 1-5" (NY open).
	Cron string
	// TZ for cron schedule. Defaults to America/New_York.
	TZ string
}

const (
	defaultEquityAnchorCron = "30 9 * * 1-5"
	defaultEquityAnchorTZ   = "America/New_York"
)

// Tick runs one pass: list linked portfolios, GET account from Alpaca, UpsertEquityAnchor for today
// (NY calendar). Errors per portfolio are logged but do not stop the rest of the batch.
func (j *EquityAnchorJob) Tick(ctx context.Context) {
	if j == nil {
		return
	}
	log := j.Log
	if log == nil {
		log = zap.NewNop()
	}
	if j.Targets == nil || j.Anchor == nil || j.NewREST == nil {
		log.Warn("equity_anchor_job_not_configured")
		return
	}
	anchorDate := TodayAnchorDateUTC(time.Now())

	targets, err := j.Targets.ListAlpacaSyncTargets(ctx)
	if err != nil {
		log.Warn("equity_anchor_list_targets_failed", zap.Error(err))
		return
	}
	if len(targets) == 0 {
		log.Info("equity_anchor_no_linked_portfolios")
		return
	}
	for _, t := range targets {
		if ctx.Err() != nil {
			return
		}
		if strings.TrimSpace(t.AlpacaKeyID) == "" || strings.TrimSpace(t.AlpacaSecretKey) == "" {
			continue
		}
		rest, err := j.NewREST(alpaca.RESTConfig{
			KeyID:     t.AlpacaKeyID,
			SecretKey: t.AlpacaSecretKey,
			BaseURL:   alpacaRESTBaseURL(t.AlpacaAccountMode, t.AlpacaBaseURL),
		})
		if err != nil {
			log.Warn("equity_anchor_rest_init_failed",
				zap.String("portfolio_id", t.PortfolioID.String()),
				zap.Error(err))
			continue
		}
		acct, err := rest.GetAccount(ctx)
		if err != nil {
			log.Warn("equity_anchor_get_account_failed",
				zap.String("portfolio_id", t.PortfolioID.String()),
				zap.Error(err))
			continue
		}
		if !acct.Equity.IsPositive() {
			log.Info("equity_anchor_skip_non_positive_equity",
				zap.String("portfolio_id", t.PortfolioID.String()),
				zap.String("equity", acct.Equity.String()))
			continue
		}
		if err := j.Anchor.UpsertEquityAnchor(ctx, t.PortfolioID, anchorDate, acct.Equity); err != nil {
			log.Warn("equity_anchor_upsert_failed",
				zap.String("portfolio_id", t.PortfolioID.String()),
				zap.Error(err))
			continue
		}
		log.Info("equity_anchor_set",
			zap.String("portfolio_id", t.PortfolioID.String()),
			zap.String("equity", acct.Equity.String()),
			zap.String("anchor_date", anchorDate.Format("2006-01-02")),
			zap.String("mode", t.AlpacaAccountMode),
		)
	}
}

// EquityAnchorScheduler manages the cron lifecycle around EquityAnchorJob.
type EquityAnchorScheduler struct {
	Job *EquityAnchorJob
	Log *zap.Logger

	mu      sync.Mutex
	cron    *cron.Cron
	started bool
}

// Start launches the cron loop. It also fires one Tick immediately so a fresh server boot after
// 9:30 AM still gets an anchor on the same day.
func (s *EquityAnchorScheduler) Start(parent context.Context) error {
	if s == nil || s.Job == nil {
		return fmt.Errorf("equity anchor scheduler: nil job")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}
	tz := s.Job.tz()
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("equity anchor scheduler: load tz %q: %w", tz, err)
	}
	c := cron.New(cron.WithLocation(loc))
	expr := s.Job.cronExpr()
	_, err = c.AddFunc(expr, func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.Job.Tick(runCtx)
	})
	if err != nil {
		return fmt.Errorf("equity anchor scheduler: parse %q: %w", expr, err)
	}
	c.Start()
	s.cron = c
	s.started = true
	if s.Log != nil {
		s.Log.Info("equity_anchor_scheduler_started", zap.String("cron", expr), zap.String("tz", tz))
	}
	// Run one tick now so a same-day server start still captures today's anchor.
	go func() {
		bootCtx, cancel := context.WithTimeout(parent, 5*time.Minute)
		defer cancel()
		s.Job.Tick(bootCtx)
		s.scheduleBootEnsureRetries(parent)
	}()
	go func() {
		<-parent.Done()
		s.Stop()
	}()
	return nil
}

func (s *EquityAnchorScheduler) scheduleBootEnsureRetries(parent context.Context) {
	if s == nil || s.Job == nil {
		return
	}
	for _, delay := range equityAnchorBootRetryDelays {
		delay := delay
		go func() {
			timer := time.NewTimer(delay)
			select {
			case <-parent.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			s.Job.EnsureTodayAllMissing(runCtx)
		}()
	}
}

// Stop halts the cron scheduler. Safe to call multiple times.
func (s *EquityAnchorScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started || s.cron == nil {
		return
	}
	stopCtx := s.cron.Stop()
	<-stopCtx.Done()
	s.started = false
	s.cron = nil
	if s.Log != nil {
		s.Log.Info("equity_anchor_scheduler_stopped")
	}
}

func (j *EquityAnchorJob) cronExpr() string {
	if v := strings.TrimSpace(j.Cron); v != "" {
		return v
	}
	return defaultEquityAnchorCron
}

func (j *EquityAnchorJob) tz() string {
	if v := strings.TrimSpace(j.TZ); v != "" {
		return v
	}
	return defaultEquityAnchorTZ
}
