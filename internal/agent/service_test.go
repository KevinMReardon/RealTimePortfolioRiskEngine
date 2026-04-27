package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type mockAgentStore struct {
	createCalls  int
	created      events.AgentSession
	toolCalls    []events.AgentSessionToolCall
	completedOK  *events.AgentSession
	completedErr *events.AgentSession
	failOnCanceledContext bool
	latest       events.AgentSession
	latestFound  bool
	list         []events.AgentSession
	replay       events.AgentSessionReplay
	replayFound  bool
}

func (m *mockAgentStore) CreateAgentSession(_ context.Context, session events.AgentSession) (events.AgentSession, error) {
	m.createCalls++
	m.created = session
	return session, nil
}
func (m *mockAgentStore) MarkAgentSessionRunning(context.Context, uuid.UUID, time.Time) error {
	return nil
}
func (m *mockAgentStore) AppendAgentSessionToolCall(_ context.Context, call events.AgentSessionToolCall) (events.AgentSessionToolCall, error) {
	m.toolCalls = append(m.toolCalls, call)
	return call, nil
}
func (m *mockAgentStore) CompleteAgentSessionSuccess(_ context.Context, session events.AgentSession) error {
	c := session
	m.completedOK = &c
	return nil
}
func (m *mockAgentStore) CompleteAgentSessionFailure(ctx context.Context, session events.AgentSession) error {
	return m.completeFailure(ctx, session)
}

func (m *mockAgentStore) completeFailure(ctx context.Context, session events.AgentSession) error {
	if m.failOnCanceledContext && ctx.Err() != nil {
		return errors.New("persist called with canceled context")
	}
	c := session
	m.completedErr = &c
	return nil
}
func (m *mockAgentStore) GetLatestAgentSessionForPortfolio(context.Context, uuid.UUID) (events.AgentSession, bool, error) {
	return m.latest, m.latestFound, nil
}
func (m *mockAgentStore) ListAgentSessionsForPortfolio(context.Context, uuid.UUID, events.AgentSessionListFilter) ([]events.AgentSession, error) {
	return m.list, nil
}
func (m *mockAgentStore) GetAgentSessionReplayByID(context.Context, uuid.UUID) (events.AgentSessionReplay, bool, error) {
	return m.replay, m.replayFound, nil
}

type mockAnthropicClient struct {
	responses []AnthropicMessageResponse
	calls     int
	onCall    func(req AnthropicMessageRequest, callN int) error
}

func (m *mockAnthropicClient) CreateMessage(_ context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	m.calls++
	if m.onCall != nil {
		if err := m.onCall(req, m.calls); err != nil {
			return AnthropicMessageResponse{}, err
		}
	}
	if m.calls > len(m.responses) {
		return AnthropicMessageResponse{}, errors.New("no mocked response")
	}
	return m.responses[m.calls-1], nil
}

type mockToolExecutor struct {
	calls []ToolCallRequest
}

func (m *mockToolExecutor) Execute(_ context.Context, call ToolCallRequest) (ToolCallResult, error) {
	m.calls = append(m.calls, call)
	return ToolCallResult{
		Output:    json.RawMessage(`{"context":"ok"}`),
		LatencyMS: 7,
		Success:   true,
	}, nil
}

type contextAwareFailingClient struct{}

func (contextAwareFailingClient) CreateMessage(ctx context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	_ = req
	<-ctx.Done()
	return AnthropicMessageResponse{}, ctx.Err()
}

type deadlineCaptureClient struct {
	minRemaining time.Duration
}

func (d deadlineCaptureClient) CreateMessage(ctx context.Context, req AnthropicMessageRequest) (AnthropicMessageResponse, error) {
	_ = req
	deadline, ok := ctx.Deadline()
	if !ok {
		return AnthropicMessageResponse{}, errors.New("missing context deadline")
	}
	remaining := time.Until(deadline)
	if remaining < d.minRemaining {
		return AnthropicMessageResponse{}, errors.New("context deadline shorter than configured timeout")
	}
	return AnthropicMessageResponse{
		StopReason: "end_turn",
		OutputText: `{
		  "market_summary":"m",
		  "portfolio_context":"p",
		  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t"}],
		  "risks_and_caveats":"rc",
		  "data_gaps":[],
		  "disclaimer":"d",
		  "used_sources":[],
		  "used_fields":[]
		}`,
		Raw: []byte(`{"stop_reason":"end_turn"}`),
	}, nil
}

func TestRunBriefing_MultiTurnToolUseThenTerminal(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "tool_use",
				ToolCalls: []AnthropicToolCall{
					{ID: "tool-1", Name: "read_only_context", Input: map[string]any{"query": "portfolio risk"}},
				},
				Raw: []byte(`{"stop_reason":"tool_use"}`),
			},
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary": "Markets are mixed.",
				  "portfolio_context": "Portfolio concentration remains elevated.",
				  "trade_ideas": [{"symbol":"AAPL","rationale":"Reduce concentration risk.","confidence":0.6,"size":"Trim 10%","stop":"Pause if trend reverses","target":"Weight below 25%"}],
				  "risks_and_caveats": "Regime shifts can invalidate assumptions.",
				  "data_gaps": ["No intraday depth metrics"],
				  "disclaimer": "Educational only.",
				  "used_sources": ["portfolio_snapshot_v1"],
				  "used_fields": ["portfolio.positions[0].quantity"]
				}`,
				Raw: []byte(`{"stop_reason":"end_turn"}`),
			},
		},
		onCall: func(req AnthropicMessageRequest, callN int) error {
			if callN == 1 && len(req.Messages) != 1 {
				return errors.New("expected single user message on first turn")
			}
			if callN == 2 {
				last := req.Messages[len(req.Messages)-1]
				if last.Role == "user" && len(last.Content) == 1 && last.Content[0].Type == "tool_result" {
					var toolResultContent string
					if err := json.Unmarshal(last.Content[0].Content, &toolResultContent); err != nil {
						return errors.New("expected tool_result content to be encoded as string JSON")
					}
					if !strings.Contains(toolResultContent, `"context":"ok"`) {
						return errors.New("expected tool_result content string to contain serialized tool output")
					}
				}
			}
			return nil
		},
	}
	toolExec := &mockToolExecutor{}
	svc := NewService(store, client, toolExec, "anthropic", "claude-test")

	out, err := svc.RunBriefing(context.Background(), RunBriefingRequest{
		PortfolioID:   uuid.New(),
		TriggerSource: "manual",
		UserInput:     json.RawMessage(`{"request":"daily briefing"}`),
	})
	if err != nil {
		t.Fatalf("RunBriefing: %v", err)
	}
	if out.Output.MarketSummary == "" {
		t.Fatalf("expected parsed output, got %+v", out.Output)
	}
	if store.completedOK == nil {
		t.Fatal("expected success completion to be persisted")
	}
	if store.completedErr != nil {
		t.Fatalf("unexpected failure completion: %+v", store.completedErr)
	}
	if len(toolExec.calls) != 1 {
		t.Fatalf("expected 1 tool execution, got %d", len(toolExec.calls))
	}
	if len(store.toolCalls) != 2 {
		t.Fatalf("expected 2 persisted tool call records (request+result), got %d", len(store.toolCalls))
	}
}

func TestRunBriefing_ValidationFailureMarksInvalidOutput(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{"market_summary":"I executed your trade."}`,
				Raw:        []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	toolExec := &mockToolExecutor{}
	svc := NewService(store, client, toolExec, "anthropic", "claude-test")

	_, err := svc.RunBriefing(context.Background(), RunBriefingRequest{
		PortfolioID:   uuid.New(),
		TriggerSource: "manual",
		UserInput:     json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if store.completedErr == nil {
		t.Fatal("expected failure completion persisted")
	}
	if store.completedErr.Status != "invalid_output" {
		t.Fatalf("failure status: got %s want invalid_output", store.completedErr.Status)
	}
}

func TestRunBriefing_RecoversFromMaxTokensTruncation(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "max_tokens",
				OutputText: "I now have all the data.\n```json\n{\"market_summary\":\"m\"",
				AssistantMessage: AnthropicMessage{
					Role: "assistant",
					Content: []AnthropicContentBlock{
						{Type: "text", Text: "I now have all the data.\n```json\n{\"market_summary\":\"m\""},
					},
				},
				Raw: []byte(`{"stop_reason":"max_tokens"}`),
			},
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary":"m",
				  "portfolio_context":"p",
				  "trade_ideas":[{"rationale":"r","confidence":0.6,"size":"s","stop":"st","target":"t"}],
				  "risks_and_caveats":"rc",
				  "data_gaps":[],
				  "disclaimer":"d",
				  "used_sources":[],
				  "used_fields":[]
				}`,
				Raw: []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	toolExec := &mockToolExecutor{}
	svc := NewService(store, client, toolExec, "anthropic", "claude-test")
	out, err := svc.RunBriefing(context.Background(), RunBriefingRequest{
		PortfolioID:   uuid.New(),
		TriggerSource: "manual",
		UserInput:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("RunBriefing: %v", err)
	}
	if out.Output.MarketSummary != "m" {
		t.Fatalf("unexpected output: %+v", out.Output)
	}
	if store.completedOK == nil {
		t.Fatal("expected successful completion after continuation")
	}
}

func TestCreateBriefingOnDemand_UsesManualTrigger(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary":"m",
				  "portfolio_context":"p",
				  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t"}],
				  "risks_and_caveats":"rc",
				  "data_gaps":[],
				  "disclaimer":"d",
				  "used_sources":[],
				  "used_fields":[]
				}`,
				Raw: []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	_, err := svc.CreateBriefingOnDemand(context.Background(), RunBriefingRequest{
		PortfolioID: uuid.New(),
		UserInput:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateBriefingOnDemand: %v", err)
	}
	if store.created.TriggerSource != "manual" {
		t.Fatalf("trigger source: got %s want manual", store.created.TriggerSource)
	}
}

func TestCreateBriefingOnDemand_DefaultsMaxTokensWhenMissing(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary":"m",
				  "portfolio_context":"p",
				  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t"}],
				  "risks_and_caveats":"rc",
				  "data_gaps":[],
				  "disclaimer":"d",
				  "used_sources":[],
				  "used_fields":[]
				}`,
				Raw: []byte(`{"stop_reason":"end_turn"}`),
			},
		},
		onCall: func(req AnthropicMessageRequest, callN int) error {
			if callN != 1 {
				return nil
			}
			if req.MaxTokens == nil || *req.MaxTokens != defaultMaxTokens {
				return errors.New("expected default max_tokens to be set")
			}
			return nil
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	_, err := svc.CreateBriefingOnDemand(context.Background(), RunBriefingRequest{
		PortfolioID: uuid.New(),
		UserInput:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateBriefingOnDemand: %v", err)
	}
	if store.created.MaxTokens == nil || *store.created.MaxTokens != defaultMaxTokens {
		t.Fatalf("stored MaxTokens = %v, want %d", store.created.MaxTokens, defaultMaxTokens)
	}
}

func TestCreateBriefingOnDemand_EnrichesPromptWithPortfolioID(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	pid := uuid.New()
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary":"m",
				  "portfolio_context":"p",
				  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t"}],
				  "risks_and_caveats":"rc",
				  "data_gaps":[],
				  "disclaimer":"d",
				  "used_sources":[],
				  "used_fields":[]
				}`,
				Raw: []byte(`{"stop_reason":"end_turn"}`),
			},
		},
		onCall: func(req AnthropicMessageRequest, callN int) error {
			if callN != 1 {
				return nil
			}
			if len(req.Messages) == 0 || len(req.Messages[0].Content) == 0 {
				return errors.New("missing initial user prompt content")
			}
			if !strings.Contains(req.Messages[0].Content[0].Text, pid.String()) {
				return errors.New("expected prompt to include portfolio_id")
			}
			return nil
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	_, err := svc.CreateBriefingOnDemand(context.Background(), RunBriefingRequest{
		PortfolioID: pid,
		UserInput:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateBriefingOnDemand: %v", err)
	}
}

func TestCreateBriefingScheduled_IdempotentPerDay(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	runDate := time.Date(2026, 4, 26, 15, 0, 0, 0, time.UTC)
	store := &mockAgentStore{
		list: []events.AgentSession{
			{
				SessionID:         uuid.New(),
				PortfolioID:       pid,
				TriggerSource:     "scheduled",
				RunDate:           runDate,
				Status:            "succeeded",
				ResponseValidated: json.RawMessage(`{"market_summary":"m","portfolio_context":"p","trade_ideas":[],"risks_and_caveats":"r","data_gaps":[],"disclaimer":"d","used_sources":[],"used_fields":[]}`),
			},
		},
	}
	svc := NewService(store, &mockAnthropicClient{}, &mockToolExecutor{}, "anthropic", "claude-test")
	out, err := svc.CreateBriefingScheduled(context.Background(), RunBriefingRequest{
		PortfolioID: pid,
		RunDate:     runDate,
		UserInput:   json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateBriefingScheduled: %v", err)
	}
	if store.createCalls != 0 {
		t.Fatalf("expected no new create due to idempotency, createCalls=%d", store.createCalls)
	}
	if out.Session.TriggerSource != "scheduled" {
		t.Fatalf("expected existing scheduled session, got trigger=%s", out.Session.TriggerSource)
	}
}

func TestBriefingReadMethods(t *testing.T) {
	t.Parallel()
	sid := uuid.New()
	pid := uuid.New()
	store := &mockAgentStore{
		latest:      events.AgentSession{SessionID: sid, PortfolioID: pid},
		latestFound: true,
		list:        []events.AgentSession{{SessionID: sid, PortfolioID: pid}},
		replay:      events.AgentSessionReplay{Session: events.AgentSession{SessionID: sid}},
		replayFound: true,
	}
	svc := NewService(store, &mockAnthropicClient{}, &mockToolExecutor{}, "anthropic", "claude-test")
	latest, found, err := svc.GetLatestBriefing(context.Background(), pid)
	if err != nil || !found || latest.SessionID != sid {
		t.Fatalf("GetLatestBriefing failed: found=%v latest=%+v err=%v", found, latest, err)
	}
	list, err := svc.ListBriefings(context.Background(), pid, events.AgentSessionListFilter{Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("ListBriefings failed: len=%d err=%v", len(list), err)
	}
	replay, found, err := svc.GetSessionReplay(context.Background(), sid)
	if err != nil || !found || replay.Session.SessionID != sid {
		t.Fatalf("GetSessionReplay failed: found=%v replay=%+v err=%v", found, replay, err)
	}
}

func TestCreateBriefingOnDemand_RedactsSensitivePayloadsBeforePersistence(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary":"m",
				  "portfolio_context":"p",
				  "trade_ideas":[{"rationale":"r","confidence":0.5,"size":"s","stop":"st","target":"t"}],
				  "risks_and_caveats":"rc",
				  "data_gaps":[],
				  "disclaimer":"d",
				  "used_sources":[],
				  "used_fields":[]
				}`,
				Raw: []byte(`{"token":"super-secret-token-value","status":"ok"}`),
			},
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	_, err := svc.CreateBriefingOnDemand(context.Background(), RunBriefingRequest{
		PortfolioID: uuid.New(),
		UserInput:   json.RawMessage(`{"api_key":"very-secret-key"}`),
	})
	if err != nil {
		t.Fatalf("CreateBriefingOnDemand: %v", err)
	}
	if strings.Contains(string(store.created.UserPrompt), "very-secret-key") {
		t.Fatalf("expected user prompt to be redacted, got %s", string(store.created.UserPrompt))
	}
	if store.completedOK == nil {
		t.Fatal("expected completed success session")
	}
	if strings.Contains(string(store.completedOK.ResponseRaw), "super-secret-token-value") {
		t.Fatalf("expected response raw to be redacted, got %s", string(store.completedOK.ResponseRaw))
	}
}

func TestRunBriefing_PersistsTerminalFailureEvenWhenRunContextCanceled(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{failOnCanceledContext: true}
	svc := NewService(store, contextAwareFailingClient{}, &mockToolExecutor{}, "anthropic", "claude-test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.RunBriefing(ctx, RunBriefingRequest{
		PortfolioID:   uuid.New(),
		TriggerSource: "manual",
		UserInput:     json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if store.completedErr == nil {
		t.Fatal("expected terminal failure to be persisted using non-canceled context")
	}
	if store.completedErr.Status != "failed" {
		t.Fatalf("completed status = %s, want failed", store.completedErr.Status)
	}
}

func TestRunBriefing_UsesConfiguredSessionTimeout(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	timeout := 90 * time.Second
	svc := NewServiceWithLoggerAndTimeout(
		store,
		deadlineCaptureClient{minRemaining: 85 * time.Second},
		&mockToolExecutor{},
		"anthropic",
		"claude-test",
		nil,
		timeout,
	)
	_, err := svc.RunBriefing(context.Background(), RunBriefingRequest{
		PortfolioID:   uuid.New(),
		TriggerSource: "manual",
		UserInput:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("RunBriefing: %v", err)
	}
}

func TestRunBriefing_FillsUnknownSizeStopTargetFromSettings(t *testing.T) {
	t.Parallel()
	store := &mockAgentStore{}
	client := &mockAnthropicClient{
		responses: []AnthropicMessageResponse{
			{
				StopReason: "end_turn",
				OutputText: `{
				  "market_summary":"m",
				  "portfolio_context":"p",
				  "trade_ideas":[{"symbol":"AAPL","rationale":"r","confidence":0.7,"size":"unknown","stop":"unknown","target":"unknown"}],
				  "risks_and_caveats":"rc",
				  "data_gaps":[],
				  "disclaimer":"d",
				  "used_sources":[],
				  "used_fields":[]
				}`,
				Raw: []byte(`{"stop_reason":"end_turn"}`),
			},
		},
	}
	svc := NewService(store, client, &mockToolExecutor{}, "anthropic", "claude-test")
	out, err := svc.RunBriefing(context.Background(), RunBriefingRequest{
		PortfolioID:   uuid.New(),
		TriggerSource: "manual",
		UserInput: json.RawMessage(`{
		  "risk_budget_per_trade_pct": 2.0,
		  "stop_style":"trailing",
		  "target_style":"r-multiple"
		}`),
	})
	if err != nil {
		t.Fatalf("RunBriefing: %v", err)
	}
	idea := out.Output.TradeIdeas[0]
	if !strings.Contains(idea.Size, "2.00%") {
		t.Fatalf("expected size default to use risk budget, got %q", idea.Size)
	}
	if !strings.Contains(strings.ToLower(idea.Stop), "trailing") {
		t.Fatalf("expected trailing stop default, got %q", idea.Stop)
	}
	if !strings.Contains(strings.ToLower(idea.Target), "2r") {
		t.Fatalf("expected r-multiple target default, got %q", idea.Target)
	}
}
