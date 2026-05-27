package agent

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type stubEligibilityLister struct {
	rows []events.AgentBriefingEligiblePortfolio
	err  error
}

func (s stubEligibilityLister) ListAgentBriefingEligiblePortfolios(ctx context.Context) ([]events.AgentBriefingEligiblePortfolio, error) {
	_ = ctx
	return s.rows, s.err
}

type countingAgentService struct {
	scheduledCalls atomic.Int32
}

func (c *countingAgentService) RunBriefing(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error) {
	_, _ = ctx, req
	return RunBriefingResult{}, nil
}
func (c *countingAgentService) CreateBriefingOnDemand(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error) {
	_, _ = ctx, req
	return RunBriefingResult{}, nil
}
func (c *countingAgentService) CreateBriefingScheduled(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error) {
	_, _ = ctx, req
	c.scheduledCalls.Add(1)
	return RunBriefingResult{}, nil
}
func (c *countingAgentService) GetLatestBriefing(ctx context.Context, portfolioID uuid.UUID) (events.AgentSession, bool, error) {
	_, _ = ctx, portfolioID
	return events.AgentSession{}, false, nil
}
func (c *countingAgentService) ListBriefings(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error) {
	_, _, _ = ctx, portfolioID, filter
	return nil, nil
}
func (c *countingAgentService) GetSessionReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error) {
	_, _ = ctx, sessionID
	return events.AgentSessionReplay{}, false, nil
}
func (c *countingAgentService) GetReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error) {
	_, _ = ctx, sessionID
	return events.AgentSessionReplay{}, false, nil
}
func (c *countingAgentService) ListPortfolioSessions(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error) {
	_, _, _ = ctx, portfolioID, filter
	return nil, nil
}

func TestRunScheduledBriefingTick_InvokesCreateBriefingScheduledPerEligible(t *testing.T) {
	t.Parallel()
	pid1, pid2 := uuid.New(), uuid.New()
	o1, o2 := uuid.New(), uuid.New()
	cat := stubEligibilityLister{rows: []events.AgentBriefingEligiblePortfolio{
		{PortfolioID: pid1, OwnerUserID: o1},
		{PortfolioID: pid2, OwnerUserID: o2},
	}}
	svc := &countingAgentService{}
	RunScheduledBriefingTick(context.Background(), zap.NewNop(), svc, cat)
	if n := svc.scheduledCalls.Load(); n != 2 {
		t.Fatalf("CreateBriefingScheduled calls = %d, want 2", n)
	}
}

func TestRunScheduledBriefingTick_DuplicateSuppressedWhenSessionRunning(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	owner := uuid.New()
	now := time.Now().UTC()
	store := &mockAgentStore{
		list: []events.AgentSession{
			{
				SessionID:     uuid.New(),
				PortfolioID:   pid,
				TriggerSource: "scheduled",
				RunDate:       now,
				// A running session should suppress the next tick to avoid concurrent duplicates.
				Status:    "running",
				CreatedAt: now.Add(-30 * time.Second),
			},
		},
	}
	svc := NewService(store, &mockAnthropicClient{}, &mockToolExecutor{}, "anthropic", "claude-test")
	cat := stubEligibilityLister{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: pid, OwnerUserID: owner}}}
	RunScheduledBriefingTick(context.Background(), zap.NewNop(), svc, cat)
	if store.createCalls != 0 {
		t.Fatalf("expected skip when session running (no new session), createCalls=%d", store.createCalls)
	}
}

func TestRunScheduledBriefingTick_AllowsNewRunAfterPreviousSucceeded(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	owner := uuid.New()
	now := time.Now().UTC()
	store := &mockAgentStore{
		list: []events.AgentSession{
			{
				SessionID:         uuid.New(),
				PortfolioID:       pid,
				TriggerSource:     "scheduled",
				RunDate:           now,
				Status:            "succeeded",
				ResponseValidated: json.RawMessage(`{"market_summary":"m","portfolio_context":"p","trade_ideas":[],"risks_and_caveats":"r","data_gaps":[],"disclaimer":"d","used_sources":[],"used_fields":[]}`),
			},
		},
	}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{"market_summary":"m","portfolio_context":"p","trade_ideas":[],"risks_and_caveats":"r","data_gaps":[],"disclaimer":"d","used_sources":[],"used_fields":[]}`,
				Raw:        []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	cat := stubEligibilityLister{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: pid, OwnerUserID: owner}}}
	RunScheduledBriefingTick(context.Background(), zap.NewNop(), svc, cat)
	// A completed session should NOT suppress the next hourly tick.
	if store.createCalls != 1 {
		t.Fatalf("expected new session after previous succeeded, createCalls=%d", store.createCalls)
	}
}

func TestRunScheduledBriefingTick_AllowsNewRunWhenInflightIsStale(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	owner := uuid.New()
	now := time.Now().UTC()
	store := &mockAgentStore{
		list: []events.AgentSession{
			{
				SessionID:     uuid.New(),
				PortfolioID:   pid,
				TriggerSource: "scheduled",
				RunDate:       now.Add(-2 * time.Hour),
				Status:        "running",
				CreatedAt:     now.Add(-90 * time.Minute),
			},
		},
	}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{"market_summary":"m","portfolio_context":"p","trade_ideas":[],"risks_and_caveats":"r","data_gaps":[],"disclaimer":"d","used_sources":[],"used_fields":[]}`,
				Raw:        []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	cat := stubEligibilityLister{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: pid, OwnerUserID: owner}}}
	RunScheduledBriefingTick(context.Background(), zap.NewNop(), svc, cat)
	if store.createCalls != 1 {
		t.Fatalf("stale in-flight session must not block tick, createCalls=%d", store.createCalls)
	}
}

func TestRunScheduledBriefingTick_ManualInflightDoesNotBlock(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	owner := uuid.New()
	now := time.Now().UTC()
	store := &mockAgentStore{
		list: []events.AgentSession{
			{
				SessionID:     uuid.New(),
				PortfolioID:   pid,
				TriggerSource: "manual",
				RunDate:       now,
				Status:        "running",
				CreatedAt:     now.Add(-1 * time.Minute),
			},
		},
	}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{"market_summary":"m","portfolio_context":"p","trade_ideas":[],"risks_and_caveats":"r","data_gaps":[],"disclaimer":"d","used_sources":[],"used_fields":[]}`,
				Raw:        []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	cat := stubEligibilityLister{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: pid, OwnerUserID: owner}}}
	RunScheduledBriefingTick(context.Background(), zap.NewNop(), svc, cat)
	if store.createCalls != 1 {
		t.Fatalf("in-flight manual session must not block scheduled tick, createCalls=%d", store.createCalls)
	}
}

func TestBriefingCronScheduler_CronTriggersExecution(t *testing.T) {
	logger := zap.NewNop()
	svc := &countingAgentService{}
	cat := stubEligibilityLister{rows: []events.AgentBriefingEligiblePortfolio{{PortfolioID: uuid.New(), OwnerUserID: uuid.New()}}}
	sched, err := NewBriefingCronScheduler(logger, svc, cat, "* * * * * *", "UTC", withCronSecondsField())
	if err != nil {
		t.Fatalf("NewBriefingCronScheduler: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sched.Run(ctx)
	}()
	time.Sleep(2500 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler Run did not exit after cancel")
	}
	if n := svc.scheduledCalls.Load(); n < 2 {
		t.Fatalf("expected at least 2 cron-driven runs, got %d", n)
	}
}

func TestNewBriefingCronScheduler_InvalidCron(t *testing.T) {
	t.Parallel()
	_, err := NewBriefingCronScheduler(zap.NewNop(), &countingAgentService{}, stubEligibilityLister{}, "not a cron", "UTC")
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNewBriefingCronScheduler_InvalidTZ(t *testing.T) {
	t.Parallel()
	_, err := NewBriefingCronScheduler(zap.NewNop(), &countingAgentService{}, stubEligibilityLister{}, "0 * * * *", "NotAReal/Timezone")
	if err == nil {
		t.Fatal("expected error for invalid timezone")
	}
}
