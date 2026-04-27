package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

func newAgentIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set INTEGRATION_DATABASE_URL or TEST_DATABASE_URL for agent integration tests")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type integrationClient struct {
	raw []byte
}

func (c *integrationClient) CreateMessage(context.Context, AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	return AnthropicMessageResponse{
		StopReason: "end_turn",
		OutputText: `{
			"market_summary":"m",
			"portfolio_context":"p",
			"trade_ideas":[{"rationale":"r","confidence":0.7,"size":"s","stop":"st","target":"t"}],
			"risks_and_caveats":"rc",
			"data_gaps":[],
			"disclaimer":"d",
			"used_sources":[],
			"used_fields":[]
		}`,
		Raw:          c.raw,
		InputTokens:  intPtr(12),
		OutputTokens: intPtr(34),
	}, nil
}

type integrationToolExec struct{}

func (integrationToolExec) Execute(context.Context, ToolCallRequest) (ToolCallResult, error) {
	return ToolCallResult{Output: json.RawMessage(`{"ok":true}`), LatencyMS: 1, Success: true}, nil
}

func TestIntegration_OnDemandRunPath(t *testing.T) {
	ctx := context.Background()
	pool := newAgentIntegrationPool(t)
	repo := events.NewPostgresStore(pool)
	portfolioID := uuid.New()
	if _, err := repo.CreatePortfolio(ctx, portfolioID, "agent-int-on-demand", "USD"); err != nil {
		t.Fatalf("CreatePortfolio: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM agent_session_tool_calls WHERE session_id IN (SELECT session_id FROM agent_sessions WHERE portfolio_id = $1)`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_sessions WHERE portfolio_id = $1`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM portfolios WHERE portfolio_id = $1`, portfolioID)
	})
	svc := NewService(repo, &integrationClient{raw: []byte(`{"token":"abc123secret"}`)}, integrationToolExec{}, "anthropic", "claude-test")
	out, err := svc.CreateBriefingOnDemand(ctx, RunBriefingRequest{
		PortfolioID: portfolioID,
		UserInput:   json.RawMessage(`{"api_key":"should-not-persist"}`),
	})
	if err != nil {
		t.Fatalf("CreateBriefingOnDemand: %v", err)
	}
	if out.Session.Status != "succeeded" {
		t.Fatalf("status=%s want succeeded", out.Session.Status)
	}
	latest, found, err := repo.GetLatestAgentSessionForPortfolio(ctx, portfolioID)
	if err != nil || !found {
		t.Fatalf("GetLatestAgentSessionForPortfolio found=%v err=%v", found, err)
	}
	if latest.Status != "succeeded" {
		t.Fatalf("latest.Status=%s want succeeded", latest.Status)
	}
}

func TestIntegration_ScheduledDuplicatePrevention(t *testing.T) {
	ctx := context.Background()
	pool := newAgentIntegrationPool(t)
	repo := events.NewPostgresStore(pool)
	portfolioID := uuid.New()
	if _, err := repo.CreatePortfolio(ctx, portfolioID, "agent-int-scheduled", "USD"); err != nil {
		t.Fatalf("CreatePortfolio: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM agent_session_tool_calls WHERE session_id IN (SELECT session_id FROM agent_sessions WHERE portfolio_id = $1)`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_sessions WHERE portfolio_id = $1`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM portfolios WHERE portfolio_id = $1`, portfolioID)
	})
	svc := NewService(repo, &integrationClient{raw: []byte(`{"ok":true}`)}, integrationToolExec{}, "anthropic", "claude-test")
	runDate := time.Date(2026, 4, 26, 14, 0, 0, 0, time.UTC)
	first, err := svc.CreateBriefingScheduled(ctx, RunBriefingRequest{
		PortfolioID: portfolioID,
		RunDate:     runDate,
	})
	if err != nil {
		t.Fatalf("first CreateBriefingScheduled: %v", err)
	}
	second, err := svc.CreateBriefingScheduled(ctx, RunBriefingRequest{
		PortfolioID: portfolioID,
		RunDate:     runDate.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("second CreateBriefingScheduled: %v", err)
	}
	if second.Session.SessionID != first.Session.SessionID {
		t.Fatalf("expected duplicate suppression to return same session id: %s vs %s", second.Session.SessionID, first.Session.SessionID)
	}
	list, err := repo.ListAgentSessionsForPortfolio(ctx, portfolioID, events.AgentSessionListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListAgentSessionsForPortfolio: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one scheduled session row, got %d", len(list))
	}
}

func intPtr(v int) *int { return &v }
