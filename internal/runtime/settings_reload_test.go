package runtime

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"

	"github.com/google/uuid"
)

type stubAgentSvc struct{}

func (stubAgentSvc) RunBriefing(context.Context, agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
	return agent.RunBriefingResult{}, nil
}
func (stubAgentSvc) CreateBriefingOnDemand(context.Context, agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
	return agent.RunBriefingResult{}, nil
}
func (stubAgentSvc) CreateBriefingScheduled(context.Context, agent.RunBriefingRequest) (agent.RunBriefingResult, error) {
	return agent.RunBriefingResult{}, nil
}
func (stubAgentSvc) GetLatestBriefing(context.Context, uuid.UUID) (events.AgentSession, bool, error) {
	return events.AgentSession{}, false, nil
}
func (stubAgentSvc) ListBriefings(context.Context, uuid.UUID, events.AgentSessionListFilter) ([]events.AgentSession, error) {
	return nil, nil
}
func (stubAgentSvc) GetSessionReplay(context.Context, uuid.UUID) (events.AgentSessionReplay, bool, error) {
	return events.AgentSessionReplay{}, false, nil
}
func (stubAgentSvc) GetReplay(context.Context, uuid.UUID) (events.AgentSessionReplay, bool, error) {
	return events.AgentSessionReplay{}, false, nil
}
func (stubAgentSvc) ListPortfolioSessions(context.Context, uuid.UUID, events.AgentSessionListFilter) ([]events.AgentSession, error) {
	return nil, nil
}

type stubEligibility struct{}

func (stubEligibility) ListAgentBriefingEligiblePortfolios(context.Context) ([]events.AgentBriefingEligiblePortfolio, error) {
	return nil, nil
}

func newTestSchedManager(t *testing.T) *SchedulerManager {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewSchedulerManager(ctx, stubAgentSvc{}, stubEligibility{}, zap.NewNop())
}

func cfgFor(enabled bool, cron, tz string) config.Config {
	return config.Config{
		AgentBriefingEnabled:          true,
		AgentBriefingSchedulerEnabled: enabled,
		AgentBriefingCron:             cron,
		AgentBriefingTZ:               tz,
	}
}

func TestSchedulerManager_StartsWhenEnabled(t *testing.T) {
	m := newTestSchedManager(t)
	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "America/New_York"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		t.Fatalf("expected scheduler running after Apply(enabled)")
	}
	if m.activeCron != "0 9-16 * * 1-5" {
		t.Fatalf("activeCron not recorded: %q", m.activeCron)
	}
	if m.activeTZ != "America/New_York" {
		t.Fatalf("activeTZ not recorded: %q", m.activeTZ)
	}
}

func TestSchedulerManager_StopsWhenDisabled(t *testing.T) {
	m := newTestSchedManager(t)
	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "America/New_York"))
	m.Apply(context.Background(), cfgFor(false, "0 9-16 * * 1-5", "America/New_York"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		t.Fatalf("expected scheduler stopped after Apply(disabled)")
	}
	if m.activeCron != "" || m.activeTZ != "" {
		t.Fatalf("expected active cron/tz cleared on stop, got cron=%q tz=%q", m.activeCron, m.activeTZ)
	}
}

func TestSchedulerManager_HotReloadsCronExpressionChange(t *testing.T) {
	m := newTestSchedManager(t)
	m.Apply(context.Background(), cfgFor(true, "0 13 * * 1-5", "America/New_York"))
	m.mu.Lock()
	firstGen := m.startGen
	m.mu.Unlock()
	if firstGen == 0 {
		t.Fatalf("expected scheduler to have started at least once")
	}

	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "America/New_York"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		t.Fatalf("expected scheduler still running after cron change")
	}
	if m.activeCron != "0 9-16 * * 1-5" {
		t.Fatalf("expected hot-reload to new cron, got %q", m.activeCron)
	}
	if m.startGen <= firstGen {
		t.Fatalf("expected start generation to advance on cron hot-reload (got %d, prev %d)", m.startGen, firstGen)
	}
}

func TestSchedulerManager_HotReloadsTZChange(t *testing.T) {
	m := newTestSchedManager(t)
	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "America/New_York"))
	m.mu.Lock()
	firstGen := m.startGen
	m.mu.Unlock()
	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "UTC"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeTZ != "UTC" {
		t.Fatalf("expected hot-reload to new tz, got %q", m.activeTZ)
	}
	if m.startGen <= firstGen {
		t.Fatalf("expected start generation to advance on tz hot-reload")
	}
}

func TestSchedulerManager_NoRestartWhenNothingChanged(t *testing.T) {
	m := newTestSchedManager(t)
	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "America/New_York"))
	m.mu.Lock()
	firstGen := m.startGen
	m.mu.Unlock()
	m.Apply(context.Background(), cfgFor(true, "0 9-16 * * 1-5", "America/New_York"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startGen != firstGen {
		t.Fatalf("expected no restart when settings unchanged, gen went %d -> %d", firstGen, m.startGen)
	}
}

func TestSchedulerManager_InvalidCronDoesNotPanic(t *testing.T) {
	m := newTestSchedManager(t)
	m.Apply(context.Background(), cfgFor(true, "not a cron", "America/New_York"))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		t.Fatalf("scheduler should not be marked running when cron parse fails")
	}
}

func TestSchedulerManager_StatusIncludesHeartbeatAndCooldown(t *testing.T) {
	m := newTestSchedManager(t)
	cfg := cfgFor(true, "*/1 * * * *", "UTC")
	m.Apply(context.Background(), cfg)
	started := time.Now().UTC()
	m.recordTickStart(started)
	finished := started.Add(time.Second)
	success := started.Add(2 * time.Second)
	m.recordTickFinish(finished, agent.ScheduledBriefingTickResult{
		SuccessfulSessions: 1,
		LastSuccessAt:      &success,
	}, 30*time.Minute)
	status := m.Status(30 * time.Minute)
	if !status.Enabled || !status.Running {
		t.Fatalf("expected scheduler enabled/running: %+v", status)
	}
	if status.LastTickAt == nil || !status.LastTickAt.Equal(started) {
		t.Fatalf("unexpected last tick: %+v", status.LastTickAt)
	}
	if status.CooldownUntil == nil || !status.CooldownUntil.Equal(success.Add(30*time.Minute)) {
		t.Fatalf("unexpected cooldown until: %+v", status.CooldownUntil)
	}
}

func TestSchedulerManager_WatchdogRestartsStaleScheduler(t *testing.T) {
	m := newTestSchedManager(t)
	cfg := cfgFor(true, "*/1 * * * *", "UTC")
	m.Apply(context.Background(), cfg)
	m.mu.Lock()
	firstGen := m.startGen
	staleNext := time.Now().UTC().Add(-5 * time.Minute)
	m.nextTickAt = &staleNext
	m.mu.Unlock()

	m.checkWatchdog(cfg)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startGen <= firstGen {
		t.Fatalf("expected watchdog restart to advance start generation, got %d <= %d", m.startGen, firstGen)
	}
	if !m.running {
		t.Fatalf("expected scheduler running after watchdog restart")
	}
}
