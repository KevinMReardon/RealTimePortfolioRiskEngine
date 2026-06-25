package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func cleanupAgentSessionData(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID, portfolioID, userID uuid.UUID) {
	t.Helper()
	if sessionID != uuid.Nil {
		if _, err := pool.Exec(ctx, `DELETE FROM agent_session_tool_calls WHERE session_id = $1`, sessionID); err != nil {
			t.Fatalf("cleanup agent_session_tool_calls: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM agent_sessions WHERE session_id = $1`, sessionID); err != nil {
			t.Fatalf("cleanup agent_sessions: %v", err)
		}
	}
	if portfolioID != uuid.Nil {
		if _, err := pool.Exec(ctx, `DELETE FROM portfolios WHERE portfolio_id = $1`, portfolioID); err != nil {
			t.Fatalf("cleanup portfolios: %v", err)
		}
	}
	if userID != uuid.Nil {
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("cleanup users: %v", err)
		}
	}
}

func TestAgentSessionLifecyclePersistence(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)

	portfolioID := uuid.New()
	userID := uuid.New()
	sessionID := uuid.New()
	cleanupAgentSessionData(t, ctx, pool, sessionID, portfolioID, userID)
	t.Cleanup(func() { cleanupAgentSessionData(t, ctx, pool, sessionID, portfolioID, userID) })

	if _, err := repo.CreatePortfolio(ctx, portfolioID, "agent-session-test", "USD"); err != nil {
		t.Fatalf("CreatePortfolio: %v", err)
	}
	if _, err := repo.CreateUser(ctx, UserAccount{
		UserID:       userID,
		DisplayName:  "Agent Tester",
		WorkEmail:    "agent-session-test@example.com",
		PasswordHash: "hash",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	temperature := decimal.RequireFromString("0.2500")
	maxTokens := 1024
	userPrompt := json.RawMessage(`{"task":"summarize","version":1}`)
	created, err := repo.CreateAgentSession(ctx, AgentSession{
		SessionID:         sessionID,
		PortfolioID:       portfolioID,
		RequestedByUserID: &userID,
		TriggerSource:     "manual",
		RunDate:           time.Date(2026, 4, 20, 9, 0, 0, 0, time.UTC),
		Status:            "queued",
		Provider:          "openai",
		Model:             "gpt-4.1",
		Temperature:       &temperature,
		MaxTokens:         &maxTokens,
		SystemPrompt:      "system prompt",
		UserPrompt:        userPrompt,
	})
	if err != nil {
		t.Fatalf("CreateAgentSession: %v", err)
	}
	if created.Status != "queued" {
		t.Fatalf("created status: got %s want queued", created.Status)
	}
	if created.RequestedByUserID == nil || *created.RequestedByUserID != userID {
		t.Fatalf("requested_by_user_id: got %v want %v", created.RequestedByUserID, userID)
	}
	if created.ToolCallCount != 0 {
		t.Fatalf("tool_call_count on create: got %d want 0", created.ToolCallCount)
	}

	startedAt := time.Now().UTC().Add(-2 * time.Second)
	if err := repo.MarkAgentSessionRunning(ctx, sessionID, startedAt); err != nil {
		t.Fatalf("MarkAgentSessionRunning: %v", err)
	}

	inputTokens := 123
	outputTokens := 456
	estimatedCost := decimal.RequireFromString("0.019876")
	completedAt := time.Now().UTC()
	if err := repo.CompleteAgentSessionSuccess(ctx, AgentSession{
		SessionID:         sessionID,
		ResponseRaw:       json.RawMessage(`{"raw":"ok"}`),
		ResponseValidated: json.RawMessage(`{"valid":true}`),
		ValidationErrors:  json.RawMessage(`[]`),
		InputTokens:       &inputTokens,
		OutputTokens:      &outputTokens,
		ToolCallCount:     2,
		EstimatedCostUSD:  &estimatedCost,
		CompletedAt:       &completedAt,
	}); err != nil {
		t.Fatalf("CompleteAgentSessionSuccess: %v", err)
	}

	latest, found, err := repo.GetLatestAgentSessionForPortfolio(ctx, portfolioID)
	if err != nil {
		t.Fatalf("GetLatestAgentSessionForPortfolio: %v", err)
	}
	if !found {
		t.Fatal("expected latest session to be found")
	}
	if latest.SessionID != sessionID {
		t.Fatalf("latest session id: got %s want %s", latest.SessionID, sessionID)
	}
	if latest.Status != "succeeded" {
		t.Fatalf("latest status: got %s want succeeded", latest.Status)
	}
	if latest.StartedAt == nil {
		t.Fatal("expected started_at after mark running")
	}
	if latest.CompletedAt == nil {
		t.Fatal("expected completed_at after success completion")
	}
	if latest.InputTokens == nil || *latest.InputTokens != inputTokens {
		t.Fatalf("input_tokens: got %v want %d", latest.InputTokens, inputTokens)
	}
	if latest.OutputTokens == nil || *latest.OutputTokens != outputTokens {
		t.Fatalf("output_tokens: got %v want %d", latest.OutputTokens, outputTokens)
	}
	if latest.EstimatedCostUSD == nil || !latest.EstimatedCostUSD.Equal(estimatedCost) {
		t.Fatalf("estimated_cost_usd: got %v want %s", latest.EstimatedCostUSD, estimatedCost)
	}

	list, err := repo.ListAgentSessionsForPortfolio(ctx, portfolioID, AgentSessionListFilter{
		Statuses: []string{"succeeded"},
		Limit:    10,
		Offset:   0,
	})
	if err != nil {
		t.Fatalf("ListAgentSessionsForPortfolio: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("filtered list length: got %d want 1", len(list))
	}
	if list[0].SessionID != sessionID {
		t.Fatalf("filtered row id: got %s want %s", list[0].SessionID, sessionID)
	}
}

func TestAgentSessionReplayOrdersToolCallsBySeqNo(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)

	portfolioID := uuid.New()
	sessionID := uuid.New()
	cleanupAgentSessionData(t, ctx, pool, sessionID, portfolioID, uuid.Nil)
	t.Cleanup(func() { cleanupAgentSessionData(t, ctx, pool, sessionID, portfolioID, uuid.Nil) })

	if _, err := repo.CreatePortfolio(ctx, portfolioID, "agent-replay-test", "USD"); err != nil {
		t.Fatalf("CreatePortfolio: %v", err)
	}
	if _, err := repo.CreateAgentSession(ctx, AgentSession{
		SessionID:     sessionID,
		PortfolioID:   portfolioID,
		TriggerSource: "scheduled",
		RunDate:       time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC),
		Status:        "running",
		Provider:      "openai",
		Model:         "gpt-4.1-mini",
		SystemPrompt:  "system",
		UserPrompt:    json.RawMessage(`{"input":"x"}`),
	}); err != nil {
		t.Fatalf("CreateAgentSession: %v", err)
	}

	if _, err := repo.AppendAgentSessionToolCall(ctx, AgentSessionToolCall{
		SessionID:  sessionID,
		SeqNo:      2,
		ToolName:   "second",
		ToolInput:  json.RawMessage(`{"n":2}`),
		ToolOutput: json.RawMessage(`{"ok":2}`),
		Success:    true,
	}); err != nil {
		t.Fatalf("AppendAgentSessionToolCall seq2: %v", err)
	}
	if _, err := repo.AppendAgentSessionToolCall(ctx, AgentSessionToolCall{
		SessionID:  sessionID,
		SeqNo:      1,
		ToolName:   "first",
		ToolInput:  json.RawMessage(`{"n":1}`),
		ToolOutput: json.RawMessage(`{"ok":1}`),
		Success:    true,
	}); err != nil {
		t.Fatalf("AppendAgentSessionToolCall seq1: %v", err)
	}

	replay, found, err := repo.GetAgentSessionReplayByID(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetAgentSessionReplayByID: %v", err)
	}
	if !found {
		t.Fatal("expected replay to be found")
	}
	if replay.Session.SessionID != sessionID {
		t.Fatalf("replay session id: got %s want %s", replay.Session.SessionID, sessionID)
	}
	if len(replay.ToolCalls) != 2 {
		t.Fatalf("tool calls length: got %d want 2", len(replay.ToolCalls))
	}
	if replay.ToolCalls[0].SeqNo != 1 || replay.ToolCalls[1].SeqNo != 2 {
		t.Fatalf("tool call order by seq_no: got [%d,%d] want [1,2]", replay.ToolCalls[0].SeqNo, replay.ToolCalls[1].SeqNo)
	}
	if replay.ToolCalls[0].ToolName != "first" || replay.ToolCalls[1].ToolName != "second" {
		t.Fatalf("tool call names order: got [%s,%s]", replay.ToolCalls[0].ToolName, replay.ToolCalls[1].ToolName)
	}
}

func TestAgentSessionScheduledAllowsMultiplePerPortfolioPerDay(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)

	portfolioID := uuid.New()
	cleanupAgentSessionData(t, ctx, pool, uuid.Nil, portfolioID, uuid.Nil)
	t.Cleanup(func() { cleanupAgentSessionData(t, ctx, pool, uuid.Nil, portfolioID, uuid.Nil) })

	if _, err := repo.CreatePortfolio(ctx, portfolioID, "agent-unique-test", "USD"); err != nil {
		t.Fatalf("CreatePortfolio: %v", err)
	}

	runDate := time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC)
	_, err := repo.CreateAgentSession(ctx, AgentSession{
		SessionID:     uuid.New(),
		PortfolioID:   portfolioID,
		TriggerSource: "scheduled",
		RunDate:       runDate,
		Status:        "queued",
		Provider:      "anthropic",
		Model:         "claude-test",
		SystemPrompt:  "system",
		UserPrompt:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("first CreateAgentSession: %v", err)
	}
	second, err := repo.CreateAgentSession(ctx, AgentSession{
		SessionID:     uuid.New(),
		PortfolioID:   portfolioID,
		TriggerSource: "scheduled",
		RunDate:       runDate.Add(3 * time.Hour),
		Status:        "queued",
		Provider:      "anthropic",
		Model:         "claude-test",
		SystemPrompt:  "system",
		UserPrompt:    json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("second CreateAgentSession: %v", err)
	}
	if second.PortfolioID != portfolioID {
		t.Fatalf("second session portfolio_id: got %s want %s", second.PortfolioID, portfolioID)
	}
}
