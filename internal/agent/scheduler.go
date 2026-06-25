package agent

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

const scheduledBriefingTickTimeout = 30 * time.Minute

// BriefingEligibilityLister lists portfolios that may receive scheduled briefings (user-owned catalog rows).
type BriefingEligibilityLister interface {
	ListAgentBriefingEligiblePortfolios(ctx context.Context) ([]events.AgentBriefingEligiblePortfolio, error)
}

type briefingCronOpts struct {
	useSeconds bool
	hooks      BriefingSchedulerHooks
}

// briefingCronOption configures BriefingCronScheduler construction.
type briefingCronOption func(*briefingCronOpts)

// withCronSecondsField parses cronExpr as a 6-field schedule including seconds (for tests).
func withCronSecondsField() briefingCronOption {
	return func(o *briefingCronOpts) {
		o.useSeconds = true
	}
}

// BriefingSchedulerHooks observes scheduler state without coupling the agent package
// to the HTTP/runtime layers.
type BriefingSchedulerHooks struct {
	OnNext       func(time.Time)
	OnTickStart  func(time.Time)
	OnTickSkip   func(time.Time, string)
	OnTickFinish func(time.Time, ScheduledBriefingTickResult)
}

// WithBriefingSchedulerHooks attaches runtime status callbacks to a scheduler.
func WithBriefingSchedulerHooks(h BriefingSchedulerHooks) briefingCronOption {
	return func(o *briefingCronOpts) {
		o.hooks = h
	}
}

// ScheduledBriefingTickResult summarizes one cron tick across all eligible portfolios.
type ScheduledBriefingTickResult struct {
	EligiblePortfolios int
	AttemptedRuns      int
	SuccessfulSessions int
	FailedRuns         int
	LastSuccessAt      *time.Time
	LastError          string
}

// BriefingCronScheduler fires on a cron schedule and invokes CreateBriefingScheduled for eligible portfolios.
type BriefingCronScheduler struct {
	log      *zap.Logger
	svc      AgentService
	catalog  BriefingEligibilityLister
	cronExpr string
	tz       string
	schedule cron.Schedule
	opts     briefingCronOpts
	tickBusy atomic.Bool
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
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if o.useSeconds {
		parser = cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	}
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("agent briefing cron: parse schedule %q: %w", cronExpr, err)
	}
	return &BriefingCronScheduler{
		log:      log,
		svc:      svc,
		catalog:  catalog,
		cronExpr: cronExpr,
		tz:       tz,
		schedule: schedule,
		opts:     o,
	}, nil
}

// Run blocks until ctx is canceled. Scheduling uses an explicit sleep loop (not robfig/cron's
// internal runner) so a long or panicking briefing cannot permanently stop future ticks.
func (s *BriefingCronScheduler) Run(ctx context.Context) {
	if s == nil {
		return
	}
	if s.schedule == nil {
		s.log.Error("agent_briefing_scheduler_not_configured")
		return
	}
	loc, err := time.LoadLocation(s.tz)
	if err != nil {
		s.log.Error("agent_briefing_scheduler_bad_tz", zap.String("tz", s.tz), zap.Error(err))
		return
	}
	s.log.Info("agent_briefing_scheduler_cron_started", zap.String("cron", s.cronExpr), zap.String("tz", s.tz))
	defer s.log.Info("agent_briefing_scheduler_cron_stopped")

	for {
		now := time.Now().In(loc)
		next := s.schedule.Next(now)
		if next.IsZero() {
			s.log.Warn("agent_briefing_scheduler_no_next_activation", zap.String("cron", s.cronExpr))
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
			continue
		}
		if s.opts.hooks.OnNext != nil {
			s.opts.hooks.OnNext(next.UTC())
		}
		delay := next.Sub(now)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.fireScheduledTick()
	}
}

func (s *BriefingCronScheduler) fireScheduledTick() {
	if s == nil {
		return
	}
	if !s.tickBusy.CompareAndSwap(false, true) {
		if s.log != nil {
			s.log.Info("agent_briefing_scheduler_tick_skipped",
				zap.String("reason", "previous_tick_still_running"),
			)
		}
		if s.opts.hooks.OnTickSkip != nil {
			s.opts.hooks.OnTickSkip(time.Now().UTC(), "previous_tick_still_running")
		}
		return
	}
	startedAt := time.Now().UTC()
	if s.log != nil {
		s.log.Info("agent_briefing_scheduler_tick", zap.Time("started_at", startedAt))
	}
	if s.opts.hooks.OnTickStart != nil {
		s.opts.hooks.OnTickStart(startedAt)
	}
	go func() {
		defer s.tickBusy.Store(false)
		runCtx, cancel := context.WithTimeout(context.Background(), scheduledBriefingTickTimeout)
		defer cancel()
		result := RunScheduledBriefingTick(runCtx, s.log, s.svc, s.catalog)
		finishedAt := time.Now().UTC()
		if s.log != nil {
			fields := []zap.Field{
				zap.Time("finished_at", finishedAt),
				zap.Int("eligible_portfolios", result.EligiblePortfolios),
				zap.Int("attempted_runs", result.AttemptedRuns),
				zap.Int("successful_sessions", result.SuccessfulSessions),
				zap.Int("failed_runs", result.FailedRuns),
			}
			if result.LastError != "" {
				fields = append(fields, zap.String("last_error", result.LastError))
			}
			s.log.Info("agent_briefing_scheduler_tick_result", fields...)
		}
		if s.opts.hooks.OnTickFinish != nil {
			s.opts.hooks.OnTickFinish(finishedAt, result)
		}
	}()
}

// RunScheduledBriefingTick lists eligible portfolios and runs CreateBriefingScheduled for each (idempotent per UTC day in the service).
func RunScheduledBriefingTick(ctx context.Context, log *zap.Logger, svc AgentService, catalog BriefingEligibilityLister) ScheduledBriefingTickResult {
	if log == nil || svc == nil || catalog == nil {
		return ScheduledBriefingTickResult{}
	}
	rows, err := catalog.ListAgentBriefingEligiblePortfolios(ctx)
	if err != nil {
		log.Warn("agent_briefing_eligible_list_failed", zap.Error(err))
		return ScheduledBriefingTickResult{LastError: err.Error()}
	}
	result := ScheduledBriefingTickResult{EligiblePortfolios: len(rows)}
	if len(rows) == 0 {
		log.Warn("agent_briefing_no_eligible_portfolios",
			zap.String("hint", "no portfolios with owner_user_id set; scheduled briefings will not run"),
		)
		return result
	}
	runDate := time.Now().UTC()
	for _, row := range rows {
		owner := row.OwnerUserID
		result.AttemptedRuns++
		out, err := svc.CreateBriefingScheduled(ctx, RunBriefingRequest{
			PortfolioID:       row.PortfolioID,
			RequestedByUserID: &owner,
			RunDate:           runDate,
			UserInput:         nil,
		})
		if err != nil {
			result.FailedRuns++
			result.LastError = err.Error()
			log.Warn("agent_briefing_scheduled_run_failed",
				zap.String("portfolio_id", row.PortfolioID.String()),
				zap.Error(err))
			continue
		}
		if out.Session.Status == "succeeded" {
			result.SuccessfulSessions++
			if out.Session.CompletedAt != nil && !out.Session.CompletedAt.IsZero() {
				v := out.Session.CompletedAt.UTC()
				result.LastSuccessAt = &v
			}
		}
	}
	return result
}
