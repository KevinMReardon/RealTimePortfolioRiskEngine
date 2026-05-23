package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

// BriefingEligibilityLister lists portfolios that may receive scheduled briefings (user-owned catalog rows).
type BriefingEligibilityLister interface {
	ListAgentBriefingEligiblePortfolios(ctx context.Context) ([]events.AgentBriefingEligiblePortfolio, error)
}

type briefingCronOpts struct {
	useSeconds bool
}

// briefingCronOption configures BriefingCronScheduler construction.
type briefingCronOption func(*briefingCronOpts)

// withCronSecondsField parses cronExpr as a 6-field schedule including seconds (for tests).
func withCronSecondsField() briefingCronOption {
	return func(o *briefingCronOpts) {
		o.useSeconds = true
	}
}

// BriefingCronScheduler runs robfig/cron jobs that invoke CreateBriefingScheduled for eligible portfolios.
type BriefingCronScheduler struct {
	log      *zap.Logger
	svc      AgentService
	catalog  BriefingEligibilityLister
	cronExpr string
	tz       string
	opts     briefingCronOpts
}

// NewBriefingCronScheduler validates expressions and returns a scheduler. Call Run from a goroutine.
func NewBriefingCronScheduler(
	log *zap.Logger,
	svc AgentService,
	catalog BriefingEligibilityLister,
	cronExpr, tz string,
	opts ...briefingCronOption,
) (*BriefingCronScheduler, error) {
	if log == nil {
		return nil, fmt.Errorf("agent briefing cron: nil logger")
	}
	if svc == nil {
		return nil, fmt.Errorf("agent briefing cron: nil agent service")
	}
	if catalog == nil {
		return nil, fmt.Errorf("agent briefing cron: nil catalog")
	}
	cronExpr = strings.TrimSpace(cronExpr)
	tz = strings.TrimSpace(tz)
	if cronExpr == "" {
		return nil, fmt.Errorf("agent briefing cron: empty cron expression")
	}
	if tz == "" {
		return nil, fmt.Errorf("agent briefing cron: empty timezone")
	}
	var o briefingCronOpts
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("agent briefing cron: load tz %q: %w", tz, err)
	}
	cronOpts := []cron.Option{cron.WithLocation(loc)}
	if o.useSeconds {
		cronOpts = append(cronOpts, cron.WithSeconds())
	}
	probe := cron.New(cronOpts...)
	if _, err := probe.AddFunc(cronExpr, func() {}); err != nil {
		return nil, fmt.Errorf("agent briefing cron: parse %q: %w", cronExpr, err)
	}
	return &BriefingCronScheduler{
		log:      log,
		svc:      svc,
		catalog:  catalog,
		cronExpr: cronExpr,
		tz:       tz,
		opts:     o,
	}, nil
}

// Run blocks until ctx is canceled, then stops the cron runner and waits for in-flight jobs.
func (s *BriefingCronScheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	loc, err := time.LoadLocation(s.tz)
	if err != nil {
		s.log.Error("agent_briefing_scheduler_bad_tz", zap.String("tz", s.tz), zap.Error(err))
		return
	}
	cronOpts := []cron.Option{cron.WithLocation(loc)}
	if s.opts.useSeconds {
		cronOpts = append(cronOpts, cron.WithSeconds())
	}
	c := cron.New(cronOpts...)
	if _, err := c.AddFunc(s.cronExpr, func() {
		runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		RunScheduledBriefingTick(runCtx, s.log, s.svc, s.catalog)
	}); err != nil {
		s.log.Error("agent_briefing_scheduler_cron_add", zap.String("cron", s.cronExpr), zap.Error(err))
		return
	}
	c.Start()
	s.log.Info("agent_briefing_scheduler_cron_started", zap.String("cron", s.cronExpr), zap.String("tz", s.tz))
	<-ctx.Done()
	stopCtx := c.Stop()
	<-stopCtx.Done()
	s.log.Info("agent_briefing_scheduler_cron_stopped")
}

// RunScheduledBriefingTick lists eligible portfolios and runs CreateBriefingScheduled for each (idempotent per UTC day in the service).
func RunScheduledBriefingTick(ctx context.Context, log *zap.Logger, svc AgentService, catalog BriefingEligibilityLister) {
	if log == nil || svc == nil || catalog == nil {
		return
	}
	rows, err := catalog.ListAgentBriefingEligiblePortfolios(ctx)
	if err != nil {
		log.Warn("agent_briefing_eligible_list_failed", zap.Error(err))
		return
	}
	if len(rows) == 0 {
		log.Warn("agent_briefing_no_eligible_portfolios",
			zap.String("hint", "no portfolios with owner_user_id set; scheduled briefings will not run"),
		)
		return
	}
	runDate := time.Now().UTC()
	for _, row := range rows {
		owner := row.OwnerUserID
		_, err := svc.CreateBriefingScheduled(ctx, RunBriefingRequest{
			PortfolioID:       row.PortfolioID,
			RequestedByUserID: &owner,
			RunDate:           runDate,
			UserInput:         nil,
		})
		if err != nil {
			log.Warn("agent_briefing_scheduled_run_failed",
				zap.String("portfolio_id", row.PortfolioID.String()),
				zap.Error(err))
		}
	}
}
