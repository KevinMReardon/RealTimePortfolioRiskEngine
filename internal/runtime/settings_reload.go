package runtime

import (
	"context"
	"fmt"
	"sync"

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
	mu         sync.Mutex
	running    bool
	cancel     context.CancelFunc
	parent     context.Context //nolint:containedctx // stored to allow restart without an explicit ctx param
	activeCron string
	activeTZ   string
	// startGen is incremented each time the inner loop is (re)started. Used by tests
	// to distinguish a successful hot-reload restart from a no-op.
	startGen int64

	agentSvc agent.AgentService
	catalog  agent.BriefingEligibilityLister
	log      *zap.Logger
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
	if m.running {
		return
	}
	m.startLocked(cfg)
}

func (m *SchedulerManager) startLocked(cfg config.Config) {
	sched, err := agent.NewBriefingCronScheduler(
		m.log, m.agentSvc, m.catalog,
		cfg.AgentBriefingCron, cfg.AgentBriefingTZ,
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
	go func() {
		sched.Run(schedCtx)
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()
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
	m.running = false
	m.activeCron = ""
	m.activeTZ = ""
	if m.log != nil {
		m.log.Info("scheduler_manager_stopped")
	}
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
