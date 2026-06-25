package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
)

// SettingsReloader reapplies app_settings over env config and updates the holder + agent runtime.
type SettingsReloader struct {
	EnvBase   config.Config
	Reader    config.AppSettingsReader
	Holder    *config.ConfigHolder
	Agent     *AgentBundle
	Scheduler *SchedulerManager
	Log       *zap.Logger
}

// Reload reads app_settings, overlays env base, and applies hot-reloadable paths.
func (r *SettingsReloader) Reload(ctx context.Context) (config.Config, error) {
	if r == nil || r.Holder == nil {
		return config.Config{}, fmt.Errorf("settings reloader not configured")
	}
	stored, err := r.Reader.ListAppSettings(ctx)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.OverlayAppSettings(r.EnvBase, stored)
	if err != nil {
		return config.Config{}, err
	}
	r.Holder.Set(cfg)
	if r.Agent != nil {
		r.Agent.Apply(cfg)
	}
	if r.Scheduler != nil {
		r.Scheduler.Apply(ctx, cfg)
	}
	if r.Log != nil {
		r.Log.Info("settings_reloaded",
			zap.String("agent_exec_mode", cfg.AgentExecMode),
			zap.Bool("paper_auto_suppressed", cfg.AgentExecPaperAutoSuppressedDueToMonitorPolicy),
			zap.Int("briefing_cooldown_minutes", int(cfg.AgentBriefingCooldown/time.Minute)),
			zap.Bool("trading_halt", cfg.TradingHalt),
			zap.String("policy_mode", string(cfg.PolicyMode)),
			zap.Bool("proposals_enabled", cfg.ProposalsEnabled),
			zap.Bool("scheduler_enabled", cfg.AgentBriefingSchedulerRuntimeEnabled()),
		)
	}
	return cfg, nil
}

// SchedulerManager can start and stop the briefing cron scheduler at runtime, and
// transparently restart it when the cron expression or timezone changes.
type SchedulerManager struct {
	mu                 sync.Mutex
	running            bool
	cancel             context.CancelFunc
	watchdogCancel     context.CancelFunc
	parent             context.Context //nolint:containedctx // stored to allow restart without an explicit ctx param
	activeCron         string
	activeTZ           string
	enabled            bool
	lastTickAt         *time.Time
	lastTickFinishedAt *time.Time
	nextTickAt         *time.Time
	lastOutcome        string
	lastError          string
	lastSuccessAt      *time.Time
	lastRestartAt      *time.Time
	// startGen is incremented each time the inner loop is (re)started. Used by tests
	// to distinguish a successful hot-reload restart from a no-op.
	startGen int64

	agentSvc agent.AgentService
	catalog  agent.BriefingEligibilityLister
	log      *zap.Logger
}

type SchedulerStatus struct {
	Enabled            bool
	Running            bool
	Cron               string
	Timezone           string
	LastTickAt         *time.Time
	LastTickFinishedAt *time.Time
	NextTickAt         *time.Time
	LastOutcome        string
	LastError          string
	LastSuccessAt      *time.Time
	CooldownUntil      *time.Time
	NextEligibleAt     *time.Time
	LastRestartAt      *time.Time
	StartGeneration    int64
}

func NewSchedulerManager(parent context.Context, svc agent.AgentService, catalog agent.BriefingEligibilityLister, log *zap.Logger) *SchedulerManager {
	return &SchedulerManager{parent: parent, agentSvc: svc, catalog: catalog, log: log}
}

// Apply starts the scheduler if cfg enables it, stops it if disabled, or restarts it
// in place when the cron expression or timezone changed while still enabled.
func (m *SchedulerManager) Apply(_ context.Context, cfg config.Config) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	want := cfg.AgentBriefingSchedulerRuntimeEnabled()
	m.enabled = want
	switch {
	case want && !m.running:
		m.startLocked(cfg)
	case !want && m.running:
		m.stopLocked()
	case want && m.running:
		// Already running — restart only if cron expr or tz has changed.
		if cfg.AgentBriefingCron != m.activeCron || cfg.AgentBriefingTZ != m.activeTZ {
			if m.log != nil {
				m.log.Info("scheduler_manager_cron_changed",
					zap.String("old_cron", m.activeCron),
					zap.String("new_cron", cfg.AgentBriefingCron),
					zap.String("old_tz", m.activeTZ),
					zap.String("new_tz", cfg.AgentBriefingTZ),
				)
			}
			m.stopLocked()
			m.startLocked(cfg)
		}
	}
}

// Start unconditionally starts the scheduler (called at boot when already enabled).
func (m *SchedulerManager) Start(cfg config.Config) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enabled = cfg.AgentBriefingSchedulerRuntimeEnabled()
	if m.running {
		return
	}
	m.startLocked(cfg)
}

func (m *SchedulerManager) startLocked(cfg config.Config) {
	m.stopWatchdogLocked()
	sched, err := agent.NewBriefingCronScheduler(
		m.log, m.agentSvc, m.catalog,
		cfg.AgentBriefingCron, cfg.AgentBriefingTZ,
		agent.WithBriefingSchedulerHooks(agent.BriefingSchedulerHooks{
			OnNext: func(next time.Time) {
				m.recordNextTick(next)
			},
			OnTickStart: func(at time.Time) {
				m.recordTickStart(at)
			},
			OnTickSkip: func(at time.Time, reason string) {
				m.recordTickSkip(at, reason)
			},
			OnTickFinish: func(at time.Time, result agent.ScheduledBriefingTickResult) {
				m.recordTickFinish(at, result, cfg.AgentBriefingCooldown)
			},
		}),
	)
	if err != nil {
		if m.log != nil {
			m.log.Warn("scheduler_manager_start_failed", zap.Error(err))
		}
		return
	}
	schedCtx, cancel := context.WithCancel(m.parent)
	m.cancel = cancel
	m.running = true
	m.activeCron = cfg.AgentBriefingCron
	m.activeTZ = cfg.AgentBriefingTZ
	m.startGen++
	gen := m.startGen
	m.lastError = ""
	m.lastOutcome = "started"
	go func() {
		sched.Run(schedCtx)
		m.mu.Lock()
		defer m.mu.Unlock()
		if gen != m.startGen {
			return
		}
		m.running = false
		if schedCtx.Err() != nil {
			return
		}
		if m.log != nil {
			m.log.Warn("scheduler_manager_run_exited_unexpectedly",
				zap.String("cron", cfg.AgentBriefingCron),
				zap.String("tz", cfg.AgentBriefingTZ),
			)
		}
	}()
	m.startWatchdogLocked(cfg)
	if m.log != nil {
		m.log.Info("scheduler_manager_started",
			zap.String("cron", cfg.AgentBriefingCron),
			zap.String("tz", cfg.AgentBriefingTZ),
		)
	}
}

func (m *SchedulerManager) stopLocked() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.stopWatchdogLocked()
	m.running = false
	m.activeCron = ""
	m.activeTZ = ""
	m.nextTickAt = nil
	if m.log != nil {
		m.log.Info("scheduler_manager_stopped")
	}
}

func (m *SchedulerManager) startWatchdogLocked(cfg config.Config) {
	if m == nil || !cfg.AgentBriefingSchedulerRuntimeEnabled() {
		return
	}
	ctx, cancel := context.WithCancel(m.parent)
	m.watchdogCancel = cancel
	go m.watchdogLoop(ctx, cfg)
}

func (m *SchedulerManager) stopWatchdogLocked() {
	if m.watchdogCancel != nil {
		m.watchdogCancel()
		m.watchdogCancel = nil
	}
}

func (m *SchedulerManager) watchdogLoop(ctx context.Context, cfg config.Config) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkWatchdog(cfg)
		}
	}
}

func (m *SchedulerManager) checkWatchdog(cfg config.Config) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.enabled || !m.running || m.activeCron == "" || m.nextTickAt == nil {
		return
	}
	buffer := 2 * time.Minute
	if cfg.AgentBriefingCooldown > buffer {
		// The heartbeat should still tick during cooldown, but keep the restart buffer
		// modest so clock jitter does not cause noisy restarts.
		buffer = 2 * time.Minute
	}
	if now.Before(m.nextTickAt.Add(buffer)) {
		return
	}
	if m.log != nil {
		m.log.Warn("scheduler_manager_watchdog_restart",
			zap.Time("expected_tick_at", *m.nextTickAt),
			zap.Time("now", now),
			zap.String("cron", m.activeCron),
			zap.String("tz", m.activeTZ),
		)
	}
	m.lastRestartAt = timePtr(now)
	m.stopLocked()
	m.enabled = cfg.AgentBriefingSchedulerRuntimeEnabled()
	m.startLocked(cfg)
}

func (m *SchedulerManager) recordNextTick(next time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next = next.UTC()
	m.nextTickAt = &next
}

func (m *SchedulerManager) recordTickStart(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at = at.UTC()
	m.lastTickAt = &at
	m.lastOutcome = "running"
	m.lastError = ""
}

func (m *SchedulerManager) recordTickSkip(at time.Time, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at = at.UTC()
	m.lastTickAt = &at
	m.lastTickFinishedAt = &at
	m.lastOutcome = "skipped:" + reason
}

func (m *SchedulerManager) recordTickFinish(at time.Time, result agent.ScheduledBriefingTickResult, cooldown time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	at = at.UTC()
	m.lastTickFinishedAt = &at
	if result.LastError != "" {
		m.lastOutcome = "completed_with_errors"
		m.lastError = result.LastError
	} else {
		m.lastOutcome = "completed"
		m.lastError = ""
	}
	if result.LastSuccessAt != nil {
		v := result.LastSuccessAt.UTC()
		m.lastSuccessAt = &v
	}
}

func (m *SchedulerManager) Status(cooldown time.Duration) SchedulerStatus {
	if m == nil {
		return SchedulerStatus{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := SchedulerStatus{
		Enabled:            m.enabled,
		Running:            m.running,
		Cron:               m.activeCron,
		Timezone:           m.activeTZ,
		LastTickAt:         cloneTime(m.lastTickAt),
		LastTickFinishedAt: cloneTime(m.lastTickFinishedAt),
		NextTickAt:         cloneTime(m.nextTickAt),
		LastOutcome:        m.lastOutcome,
		LastError:          m.lastError,
		LastSuccessAt:      cloneTime(m.lastSuccessAt),
		LastRestartAt:      cloneTime(m.lastRestartAt),
		StartGeneration:    m.startGen,
	}
	if cooldown > 0 && out.LastSuccessAt != nil {
		cooldownUntil := out.LastSuccessAt.Add(cooldown).UTC()
		out.CooldownUntil = &cooldownUntil
		if out.NextTickAt != nil && out.NextTickAt.After(cooldownUntil) {
			nextEligible := out.NextTickAt.UTC()
			out.NextEligibleAt = &nextEligible
		} else {
			out.NextEligibleAt = &cooldownUntil
		}
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	t = t.UTC()
	return &t
}

func cloneTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	return timePtr(*t)
}

// AgentBundle holds references for hot-reload after settings PATCH.
type AgentBundle struct {
	Holder       *config.ConfigHolder
	Service      *agent.Service
	Materializer *agent.BriefingProposalMaterializer
	// PaperAutoFactory builds a runner when paper_auto is active; nil runner when off.
	PaperAutoFactory func(cfg config.Config) *agent.PaperAutoRunner
}

// Apply updates agent limits and paper_auto without restart.
func (b *AgentBundle) Apply(cfg config.Config) {
	if b == nil {
		return
	}
	if b.Service != nil {
		b.Service.WithLimits(cfg.AgentMaxTurns, cfg.AgentMaxToolCalls)
		b.Service.SetScheduledCooldown(cfg.AgentBriefingCooldown)
		var runner *agent.PaperAutoRunner
		if b.PaperAutoFactory != nil && cfg.AgentPaperAutoRuntimeEnabled() {
			runner = b.PaperAutoFactory(cfg)
		}
		b.Service.SetPaperAuto(runner)
	}
	if b.Materializer != nil && b.Holder != nil {
		b.Materializer.Runtime = b.Holder
	}
}
