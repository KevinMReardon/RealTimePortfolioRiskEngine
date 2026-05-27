package agent

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

func newServiceForActiveSchedTest(list []events.AgentSession) *Service {
	store := &mockAgentStore{list: list}
	return &Service{
		store:          store,
		log:            zap.NewNop(),
		sessionTimeout: 45 * time.Second,
	}
}

func TestFindActiveScheduledSession_IgnoresManualInflight(t *testing.T) {
	portfolioID := uuid.New()
	now := time.Now().UTC()
	svc := newServiceForActiveSchedTest([]events.AgentSession{
		{
			SessionID:     uuid.New(),
			PortfolioID:   portfolioID,
			TriggerSource: "manual",
			Status:        "running",
			CreatedAt:     now.Add(-1 * time.Minute),
		},
	})
	_, found, err := svc.findActiveScheduledSession(context.Background(), portfolioID, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("manual session should not block scheduled tick")
	}
}

func TestFindActiveScheduledSession_IgnoresCompleted(t *testing.T) {
	portfolioID := uuid.New()
	now := time.Now().UTC()
	svc := newServiceForActiveSchedTest([]events.AgentSession{
		{SessionID: uuid.New(), PortfolioID: portfolioID, TriggerSource: "scheduled", Status: "succeeded", CreatedAt: now.Add(-2 * time.Minute)},
		{SessionID: uuid.New(), PortfolioID: portfolioID, TriggerSource: "scheduled", Status: "failed", CreatedAt: now.Add(-3 * time.Minute)},
	})
	_, found, err := svc.findActiveScheduledSession(context.Background(), portfolioID, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("completed sessions should not suppress new tick")
	}
}

func TestFindActiveScheduledSession_DetectsFreshScheduled(t *testing.T) {
	portfolioID := uuid.New()
	now := time.Now().UTC()
	live := uuid.New()
	svc := newServiceForActiveSchedTest([]events.AgentSession{
		{SessionID: live, PortfolioID: portfolioID, TriggerSource: "scheduled", Status: "running", CreatedAt: now.Add(-30 * time.Second)},
	})
	got, found, err := svc.findActiveScheduledSession(context.Background(), portfolioID, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected to find live scheduled session")
	}
	if got.SessionID != live {
		t.Fatalf("returned wrong session: got=%s want=%s", got.SessionID, live)
	}
}

func TestFindActiveScheduledSession_IgnoresStaleScheduled(t *testing.T) {
	portfolioID := uuid.New()
	now := time.Now().UTC()
	svc := newServiceForActiveSchedTest([]events.AgentSession{
		{
			SessionID:     uuid.New(),
			PortfolioID:   portfolioID,
			TriggerSource: "scheduled",
			Status:        "running",
			// session timeout is 45s so staleAfter = max(10m, 90s) = 10m.
			CreatedAt: now.Add(-30 * time.Minute),
		},
	})
	_, found, err := svc.findActiveScheduledSession(context.Background(), portfolioID, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("stale scheduled session should not block new tick")
	}
}

func TestFindActiveScheduledSession_UsesStartedAtWhenPresent(t *testing.T) {
	portfolioID := uuid.New()
	now := time.Now().UTC()
	// CreatedAt is old, but started_at is recent → treat as fresh.
	started := now.Add(-1 * time.Minute)
	svc := newServiceForActiveSchedTest([]events.AgentSession{
		{
			SessionID:     uuid.New(),
			PortfolioID:   portfolioID,
			TriggerSource: "scheduled",
			Status:        "running",
			CreatedAt:     now.Add(-2 * time.Hour),
			StartedAt:     &started,
		},
	})
	_, found, err := svc.findActiveScheduledSession(context.Background(), portfolioID, now)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected freshly-started session to be detected")
	}
}
