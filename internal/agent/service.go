package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
)

// Service is the default AgentService implementation.
type Service struct {
	store        AgentStore
	client       AnthropicClient
	toolExecutor ToolExecutor
	provider     string
	model        string
	log          *zap.Logger
	sessionTimeout time.Duration
	materializer   ProposalMaterializer
	paperAutoMu    sync.RWMutex
	paperAuto      *PaperAutoRunner
	maxTurns       int
	maxToolCalls   int
}

// SetPaperAuto swaps the post-briefing autonomous submit runner (nil disables).
func (s *Service) SetPaperAuto(r *PaperAutoRunner) {
	if s == nil {
		return
	}
	s.paperAutoMu.Lock()
	s.paperAuto = r
	s.paperAutoMu.Unlock()
}

func (s *Service) paperAutoRunner() *PaperAutoRunner {
	if s == nil {
		return nil
	}
	s.paperAutoMu.RLock()
	defer s.paperAutoMu.RUnlock()
	return s.paperAuto
}

// WithLimits sets the per-session turn and tool-call caps from config.
// Returns the receiver so it can be chained after the constructor.
// Zero values fall back to the package defaults.
func (s *Service) WithLimits(maxTurns, maxToolCalls int) *Service {
	if s == nil {
		return s
	}
	if maxTurns > 0 {
		s.maxTurns = maxTurns
	}
	if maxToolCalls > 0 {
		s.maxToolCalls = maxToolCalls
	}
	return s
}

const (
	defaultMaxTurns      = 6
	defaultMaxToolCalls  = 12
	defaultTimeoutBudget = 45 * time.Second
	defaultMaxTokens     = 4096
	// Large response_raw + jsonb writes can exceed a few seconds under Docker/remote DB.
	terminalPersistBudget = 30 * time.Second
)

func NewService(store AgentStore, client AnthropicClient, toolExecutor ToolExecutor, provider, model string) *Service {
	return NewServiceWithLogger(store, client, toolExecutor, provider, model, nil)
}

func NewServiceWithLogger(store AgentStore, client AnthropicClient, toolExecutor ToolExecutor, provider, model string, log *zap.Logger) *Service {
	return NewServiceWithLoggerAndTimeout(store, client, toolExecutor, provider, model, log, defaultTimeoutBudget, nil, nil)
}

func NewServiceWithLoggerAndTimeout(
	store AgentStore,
	client AnthropicClient,
	toolExecutor ToolExecutor,
	provider, model string,
	log *zap.Logger,
	sessionTimeout time.Duration,
	materializer ProposalMaterializer,
	paperAuto *PaperAutoRunner,
) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	if sessionTimeout <= 0 {
		sessionTimeout = defaultTimeoutBudget
	}
	return &Service{
		store:          store,
		client:         client,
		toolExecutor:   toolExecutor,
		provider:       provider,
		model:          model,
		log:            log,
		sessionTimeout: sessionTimeout,
		materializer:   materializer,
		paperAuto:      paperAuto,
	}
}

func (s *Service) RunBriefing(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error) {
	return s.runBriefing(ctx, req, false)
}

func (s *Service) CreateBriefingOnDemand(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error) {
	req.TriggerSource = "manual"
	if req.RunDate.IsZero() {
		req.RunDate = time.Now().UTC()
	}
	return s.runBriefing(ctx, req, false)
}

func (s *Service) CreateBriefingScheduled(ctx context.Context, req RunBriefingRequest) (RunBriefingResult, error) {
	req.TriggerSource = "scheduled"
	if req.RunDate.IsZero() {
		req.RunDate = time.Now().UTC()
	}
	return s.runBriefing(ctx, req, true)
}

func (s *Service) runBriefing(ctx context.Context, req RunBriefingRequest, enforceScheduledDailyIdempotency bool) (result RunBriefingResult, retErr error) {
	if s == nil {
		return RunBriefingResult{}, fmt.Errorf("agent service: nil receiver")
	}
	if s.store == nil {
		return RunBriefingResult{}, fmt.Errorf("agent service: nil store")
	}
	if s.client == nil {
		return RunBriefingResult{}, fmt.Errorf("agent service: nil anthropic client")
	}
	if s.toolExecutor == nil {
		return RunBriefingResult{}, fmt.Errorf("agent service: nil tool executor")
	}
	sessionID := uuid.New()
	type sessionCacheCleaner interface {
		ClearSessionCache(sessionID string)
	}
	if cleaner, ok := s.toolExecutor.(sessionCacheCleaner); ok {
		defer cleaner.ClearSessionCache(sessionID.String())
	}
	runDate := req.RunDate.UTC()
	if runDate.IsZero() {
		runDate = time.Now().UTC()
	}
	trigger := req.TriggerSource
	if trigger == "" {
		trigger = "manual"
	}
	if enforceScheduledDailyIdempotency && trigger == "scheduled" {
		existing, found, err := s.findActiveScheduledSession(ctx, req.PortfolioID, time.Now().UTC())
		if err != nil {
			return RunBriefingResult{}, fmt.Errorf("check scheduled idempotency: %w", err)
		}
		if found {
			observability.ObserveAgentSessionOutcome("deduped", trigger)
			s.log.Warn("agent_session_skip_in_progress",
				zap.String("active_session_id", existing.SessionID.String()),
				zap.String("portfolio_id", req.PortfolioID.String()),
				zap.String("trigger_source", trigger),
				zap.String("active_status", existing.Status),
			)
			out := BriefingOutput{}
			if len(existing.ResponseValidated) > 0 {
				_ = json.Unmarshal(existing.ResponseValidated, &out)
			}
			return RunBriefingResult{
				Session: existing,
				Output:  out,
			}, nil
		}
	}
	var tempDec *decimal.Decimal
	if req.Temperature != nil {
		v := decimal.NewFromFloat(*req.Temperature).Round(4)
		tempDec = &v
	}
	effectiveUserInput, err := enrichBriefingUserInput(req)
	if err != nil {
		return RunBriefingResult{}, fmt.Errorf("enrich briefing user input: %w", err)
	}
	effectiveMaxTokens := req.MaxTokens
	if effectiveMaxTokens == nil || *effectiveMaxTokens <= 0 {
		effectiveMaxTokens = defaultIntPtr(defaultMaxTokens)
	}
	redactedUserInput := redactJSON(effectiveUserInput)
	redactedSystemPrompt := RedactText(BriefingSystemPrompt())
	session, err := s.store.CreateAgentSession(ctx, events.AgentSession{
		SessionID:         sessionID,
		PortfolioID:       req.PortfolioID,
		RequestedByUserID: req.RequestedByUserID,
		TriggerSource:     trigger,
		RunDate:           runDate,
		Status:            "queued",
		Provider:          s.provider,
		Model:             nonEmpty(req.Model, s.model),
		Temperature:       tempDec,
		MaxTokens:         effectiveMaxTokens,
		SystemPrompt:      redactedSystemPrompt,
		UserPrompt:        redactedUserInput,
	})
	if err != nil {
		return RunBriefingResult{}, err
	}
	s.log.Info("agent_session_created",
		zap.String("session_id", sessionID.String()),
		zap.String("portfolio_id", req.PortfolioID.String()),
		zap.String("trigger_source", trigger),
		zap.String("status", "queued"),
	)
	startedAt := time.Now().UTC()
	if err := s.store.MarkAgentSessionRunning(ctx, sessionID, startedAt); err != nil {
		return RunBriefingResult{}, fmt.Errorf("agent run mark running: %w", err)
	}
	s.log.Info("agent_session_running",
		zap.String("session_id", sessionID.String()),
		zap.String("portfolio_id", req.PortfolioID.String()),
		zap.String("trigger_source", trigger),
		zap.String("status", "running"),
	)
	terminalPersisted := false
	defer func() {
		if retErr == nil || terminalPersisted {
			return
		}
		errorCode := "INTERNAL_ERROR"
		if errors.Is(retErr, context.DeadlineExceeded) || errors.Is(retErr, context.Canceled) {
			errorCode = "TIMEOUT"
		}
		persistErr := s.completeFailureWithPersistenceContext(events.AgentSession{
			SessionID:     sessionID,
			Status:        "failed",
			ToolCallCount: 0,
			ErrorCode:     strPtr(errorCode),
			ErrorMessage:  strPtr(retErr.Error()),
			CompletedAt:   timePtr(time.Now().UTC()),
		})
		if persistErr != nil {
			s.log.Warn("agent_session_terminalizer_failed",
				zap.String("session_id", sessionID.String()),
				zap.String("portfolio_id", req.PortfolioID.String()),
				zap.String("trigger_source", trigger),
				zap.Error(persistErr),
			)
			return
		}
		observability.ObserveAgentSessionOutcome("failed", trigger)
		s.log.Warn("agent_session_failed",
			zap.String("session_id", sessionID.String()),
			zap.String("portfolio_id", req.PortfolioID.String()),
			zap.String("trigger_source", trigger),
			zap.String("status", "failed"),
			zap.String("error", retErr.Error()),
		)
	}()

	runCtx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
	defer cancel()

	modelName := nonEmpty(req.Model, s.model)
	if strings.TrimSpace(modelName) == "" {
		modelName = "claude-3-5-sonnet-latest"
	}
	systemPrompt := BriefingSystemPrompt()
	portfolioCtx, marketCtx := s.bootstrapBriefingContext(ctx, req.PortfolioID)
	userPrompt := BuildBriefingUserPromptFromContext(portfolioCtx, nil, marketCtx, effectiveUserInput)
	if len(marketCtx) > 2 {
		s.log.Info("briefing_bootstrap_loaded",
			zap.String("portfolio_id", req.PortfolioID.String()),
			zap.Int("market_context_bytes", len(marketCtx)),
		)
	}
	temperature := req.Temperature
	maxTokens := effectiveMaxTokens
	messages := []AnthropicMessage{
		{
			Role: "user",
			Content: []AnthropicContentBlock{
				{Type: "text", Text: userPrompt},
			},
		},
	}

	effectiveMaxTurns := s.maxTurns
	if effectiveMaxTurns <= 0 {
		effectiveMaxTurns = defaultMaxTurns
	}
	effectiveMaxToolCalls := s.maxToolCalls
	if effectiveMaxToolCalls <= 0 {
		effectiveMaxToolCalls = defaultMaxToolCalls
	}

	totalToolCalls := 0
	seqNo := 1
	var lastResp AnthropicMessageResponse
	for turn := 0; turn < effectiveMaxTurns; turn++ {
		resp, err := s.client.CreateMessage(runCtx, AnthropicMessageRequest{
			Model:       modelName,
			System:      systemPrompt,
			Temperature: temperature,
			MaxTokens:   maxTokens,
			Messages:    messages,
			Tools:       AnthropicToolCatalog(),
		})
		if err != nil {
			failErr := s.completeFailureWithPersistenceContext(events.AgentSession{
				SessionID:         sessionID,
				Status:            "failed",
				ToolCallCount:     totalToolCalls,
				ErrorCode:         strPtr("PROVIDER_ERROR"),
				ErrorMessage:      strPtr(err.Error()),
				CompletedAt:       timePtr(time.Now().UTC()),
				ResponseRaw:       nil,
				ResponseValidated: nil,
			})
			if failErr != nil {
				return RunBriefingResult{}, fmt.Errorf("agent provider error: %w (plus complete failure error: %v)", err, failErr)
			}
			terminalPersisted = true
			observability.ObserveAgentSessionOutcome("failed", trigger)
			s.log.Warn("agent_session_failed",
				zap.String("session_id", sessionID.String()),
				zap.String("portfolio_id", req.PortfolioID.String()),
				zap.String("trigger_source", trigger),
				zap.String("status", "failed"),
				zap.String("error", err.Error()),
			)
			return RunBriefingResult{}, fmt.Errorf("agent provider call: %w", err)
		}
		lastResp = resp
		if resp.StopReason == "max_tokens" {
			if len(resp.AssistantMessage.Content) > 0 {
				messages = append(messages, resp.AssistantMessage)
			} else if strings.TrimSpace(resp.OutputText) != "" {
				messages = append(messages, AnthropicMessage{
					Role: "assistant",
					Content: []AnthropicContentBlock{
						{Type: "text", Text: resp.OutputText},
					},
				})
			}
			messages = append(messages, AnthropicMessage{
				Role: "user",
				Content: []AnthropicContentBlock{
					{
						Type: "text",
						Text: "Your previous reply was truncated. Return ONLY compact valid JSON matching the required schema. No prose, no markdown, no code fences, and keep each field concise.",
					},
				},
			})
			continue
		}
		if resp.StopReason != "tool_use" {
			break
		}
		if len(resp.ToolCalls) == 0 {
			if err := s.completeFailureWithPersistenceContext(events.AgentSession{
				SessionID:     sessionID,
				Status:        "invalid_output",
				ToolCallCount: totalToolCalls,
				ErrorCode:     strPtr("EMPTY_TOOL_USE"),
				ErrorMessage:  strPtr("model returned tool_use stop reason with no tool calls"),
				CompletedAt:   timePtr(time.Now().UTC()),
				ResponseRaw:   resp.Raw,
			}); err != nil {
				return RunBriefingResult{}, fmt.Errorf("complete invalid_output: %w", err)
			}
			terminalPersisted = true
			observability.ObserveAgentSessionOutcome("invalid_output", trigger)
			observability.IncAgentValidationFailure()
			return RunBriefingResult{}, fmt.Errorf("agent invalid tool response: empty tool calls")
		}

		if len(resp.AssistantMessage.Content) > 0 {
			messages = append(messages, resp.AssistantMessage)
		} else {
			// Fallback for tests/stubs that return ToolCalls but not full assistant content.
			assistantToolUseBlocks := make([]AnthropicContentBlock, 0, len(resp.ToolCalls))
			for _, tc := range resp.ToolCalls {
				inputRaw, err := marshalToolInput(tc.Input)
				if err != nil {
					return RunBriefingResult{}, fmt.Errorf("marshal assistant tool_use input: %w", err)
				}
				assistantToolUseBlocks = append(assistantToolUseBlocks, AnthropicContentBlock{
					Type:  "tool_use",
					ID:    strings.TrimSpace(tc.ID),
					Name:  strings.TrimSpace(tc.Name),
					Input: inputRaw,
				})
			}
			messages = append(messages, AnthropicMessage{
				Role:    "assistant",
				Content: assistantToolUseBlocks,
			})
		}

		toolResultBlocks := make([]AnthropicContentBlock, 0, len(resp.ToolCalls))
		for _, toolCall := range resp.ToolCalls {
			if totalToolCalls >= effectiveMaxToolCalls {
				if err := s.completeFailureWithPersistenceContext(events.AgentSession{
					SessionID:     sessionID,
					Status:        "rate_limited",
					ToolCallCount: totalToolCalls,
					ErrorCode:     strPtr("MAX_TOOL_CALLS_EXCEEDED"),
					ErrorMessage:  strPtr("tool call limit exceeded"),
					CompletedAt:   timePtr(time.Now().UTC()),
					ResponseRaw:   resp.Raw,
				}); err != nil {
					return RunBriefingResult{}, fmt.Errorf("complete rate_limited: %w", err)
				}
				terminalPersisted = true
				return RunBriefingResult{}, fmt.Errorf("agent tool limit exceeded")
			}
			inputRaw, err := marshalToolInput(toolCall.Input)
			if err != nil {
				return RunBriefingResult{}, fmt.Errorf("marshal tool input: %w", err)
			}
			redactedInput := redactJSON(inputRaw)
			_, err = s.store.AppendAgentSessionToolCall(runCtx, events.AgentSessionToolCall{
				SessionID: sessionID,
				SeqNo:     seqNo,
				ToolName:  "tool_use_request:" + strings.TrimSpace(toolCall.Name),
				ToolInput: redactedInput,
				Success:   false,
			})
			if err != nil {
				return RunBriefingResult{}, fmt.Errorf("persist tool-use request: %w", err)
			}
			seqNo++

			start := time.Now()
			toolRes, execErr := s.toolExecutor.Execute(runCtx, ToolCallRequest{
				SessionID: sessionID.String(),
				SeqNo:     seqNo,
				ToolName:  toolCall.Name,
				Input:     inputRaw,
			})
			latency := int(time.Since(start).Milliseconds())
			if latency < 0 {
				latency = 0
			}
			if execErr != nil {
				toolRes = ToolCallResult{
					Output:    json.RawMessage(`{"error":"tool execution failed"}`),
					LatencyMS: latency,
					Success:   false,
					Error:     execErr.Error(),
				}
			}
			latMS := toolRes.LatencyMS
			if latMS <= 0 {
				latMS = latency
			}
			redactedOutput := redactJSON(toolRes.Output)
			_, err = s.store.AppendAgentSessionToolCall(runCtx, events.AgentSessionToolCall{
				SessionID:    sessionID,
				SeqNo:        seqNo,
				ToolName:     "tool_result:" + strings.TrimSpace(toolCall.Name),
				ToolInput:    redactedInput,
				ToolOutput:   redactedOutput,
				LatencyMS:    &latMS,
				Success:      toolRes.Success,
				ErrorMessage: nullableStr(toolRes.Error),
			})
			if err != nil {
				return RunBriefingResult{}, fmt.Errorf("persist tool result: %w", err)
			}
			seqNo++
			totalToolCalls++
			toolStatus := "failed"
			if toolRes.Success {
				toolStatus = "ok"
			}
			observability.ObserveAgentToolCall(strings.TrimSpace(toolCall.Name), toolStatus, time.Duration(latMS)*time.Millisecond)
			s.log.Info("agent_tool_call",
				zap.String("session_id", sessionID.String()),
				zap.String("portfolio_id", req.PortfolioID.String()),
				zap.String("trigger_source", trigger),
				zap.String("tool_name", strings.TrimSpace(toolCall.Name)),
				zap.Int("latency_ms", latMS),
				zap.String("status", toolStatus),
			)

			toolResultBlocks = append(toolResultBlocks, AnthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: strings.TrimSpace(toolCall.ID),
				Content:   toolResultContentAsJSONString(toolRes.Output),
			})
		}
		messages = append(messages, AnthropicMessage{
			Role:    "user",
			Content: toolResultBlocks,
		})
	}

	if lastResp.StopReason == "tool_use" {
		if err := s.completeFailureWithPersistenceContext(events.AgentSession{
			SessionID:     sessionID,
			Status:        "failed",
			ToolCallCount: totalToolCalls,
			ErrorCode:     strPtr("MAX_TURNS_EXCEEDED"),
			ErrorMessage:  strPtr("model did not produce terminal response within turn budget"),
			CompletedAt:   timePtr(time.Now().UTC()),
			ResponseRaw:   redactJSON(lastResp.Raw),
		}); err != nil {
			return RunBriefingResult{}, fmt.Errorf("complete max turns failure: %w", err)
		}
		terminalPersisted = true
		observability.ObserveAgentSessionOutcome("failed", trigger)
		return RunBriefingResult{}, fmt.Errorf("agent max turns exceeded")
	}

	validated, valErr := ValidateBriefingOutput(json.RawMessage(lastResp.OutputText))
	if valErr != nil {
		errText := valErr.Error()
		validationJSON, _ := json.Marshal(validationIssuesFromError(valErr))
		if err := s.completeFailureWithPersistenceContext(events.AgentSession{
			SessionID:        sessionID,
			Status:           "invalid_output",
			ToolCallCount:    totalToolCalls,
			ErrorCode:        strPtr("VALIDATION_FAILED"),
			ErrorMessage:     &errText,
			ResponseRaw:      redactJSON(lastResp.Raw),
			ValidationErrors: redactJSON(validationJSON),
			CompletedAt:      timePtr(time.Now().UTC()),
		}); err != nil {
			s.log.Warn("agent_session_invalid_output_persist_failed",
				zap.String("session_id", sessionID.String()),
				zap.String("portfolio_id", req.PortfolioID.String()),
				zap.Error(err),
			)
			observability.IncAgentValidationFailure()
			// Always return the validation error for API mapping (422); DB row may be finalized by defer.
			return RunBriefingResult{}, fmt.Errorf("agent output validation failed: %w", valErr)
		}
		terminalPersisted = true
		observability.ObserveAgentSessionOutcome("invalid_output", trigger)
		observability.IncAgentValidationFailure()
		s.log.Warn("agent_session_failed",
			zap.String("session_id", sessionID.String()),
			zap.String("portfolio_id", req.PortfolioID.String()),
			zap.String("trigger_source", trigger),
			zap.String("status", "invalid_output"),
		)
		return RunBriefingResult{}, fmt.Errorf("agent output validation failed: %w", valErr)
	}
	validated = applyTradeIdeaDefaults(validated, effectiveUserInput)

	validatedJSON, err := json.Marshal(validated)
	if err != nil {
		return RunBriefingResult{}, fmt.Errorf("marshal validated output: %w", err)
	}
	estimatedCost := estimateCostUSD(lastResp.InputTokens, lastResp.OutputTokens)
	if err := s.completeSuccessWithPersistenceContext(events.AgentSession{
		SessionID:         sessionID,
		ToolCallCount:     totalToolCalls,
		ResponseRaw:       redactJSON(lastResp.Raw),
		ResponseValidated: validatedJSON,
		InputTokens:       lastResp.InputTokens,
		OutputTokens:      lastResp.OutputTokens,
		EstimatedCostUSD:  estimatedCost,
		CompletedAt:       timePtr(time.Now().UTC()),
	}); err != nil {
		return RunBriefingResult{}, fmt.Errorf("complete agent session success: %w", err)
	}
	if s.materializer != nil {
		matCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		propIDs, err := s.materializer.Materialize(matCtx, req.PortfolioID, sessionID, validated)
		if err != nil {
			s.log.Warn("proposal_materialize_failed", zap.Error(err))
		} else if pa := s.paperAutoRunner(); pa != nil && len(propIDs) > 0 {
			go pa.RunAfterMaterialize(context.Background(), req.PortfolioID, propIDs)
		}
	}
	terminalPersisted = true
	observability.ObserveAgentSessionOutcome("succeeded", trigger)
	observability.AddAgentTokenUsage(lastResp.InputTokens, lastResp.OutputTokens)
	s.log.Info("agent_session_completed",
		zap.String("session_id", sessionID.String()),
		zap.String("portfolio_id", req.PortfolioID.String()),
		zap.String("trigger_source", trigger),
		zap.String("status", "succeeded"),
		zap.Int("tool_call_count", totalToolCalls),
	)
	session.Status = "succeeded"
	session.ToolCallCount = totalToolCalls
	session.ResponseRaw = lastResp.Raw
	session.ResponseValidated = validatedJSON
	session.InputTokens = lastResp.InputTokens
	session.OutputTokens = lastResp.OutputTokens
	session.EstimatedCostUSD = estimatedCost
	return RunBriefingResult{Session: session, Output: validated}, nil
}

func (s *Service) completeFailureWithPersistenceContext(session events.AgentSession) error {
	persistCtx, cancel := context.WithTimeout(context.Background(), terminalPersistBudget)
	defer cancel()
	return s.store.CompleteAgentSessionFailure(persistCtx, session)
}

func (s *Service) completeSuccessWithPersistenceContext(session events.AgentSession) error {
	persistCtx, cancel := context.WithTimeout(context.Background(), terminalPersistBudget)
	defer cancel()
	return s.store.CompleteAgentSessionSuccess(persistCtx, session)
}

func (s *Service) GetLatestBriefing(ctx context.Context, portfolioID uuid.UUID) (events.AgentSession, bool, error) {
	if s == nil || s.store == nil {
		return events.AgentSession{}, false, fmt.Errorf("agent service: nil store")
	}
	return s.store.GetLatestAgentSessionForPortfolio(ctx, portfolioID)
}

func (s *Service) ListBriefings(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("agent service: nil store")
	}
	return s.store.ListAgentSessionsForPortfolio(ctx, portfolioID, filter)
}

func (s *Service) GetSessionReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error) {
	if s == nil || s.store == nil {
		return events.AgentSessionReplay{}, false, fmt.Errorf("agent service: nil store")
	}
	return s.store.GetAgentSessionReplayByID(ctx, sessionID)
}

func (s *Service) GetReplay(ctx context.Context, sessionID uuid.UUID) (events.AgentSessionReplay, bool, error) {
	return s.GetSessionReplay(ctx, sessionID)
}

func (s *Service) ListPortfolioSessions(ctx context.Context, portfolioID uuid.UUID, filter events.AgentSessionListFilter) ([]events.AgentSession, error) {
	return s.ListBriefings(ctx, portfolioID, filter)
}

func nonEmpty(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}

func marshalToolInput(v any) (json.RawMessage, error) {
	switch vv := v.(type) {
	case nil:
		return json.RawMessage(`{}`), nil
	case json.RawMessage:
		if len(vv) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return append(json.RawMessage(nil), vv...), nil
	case []byte:
		if len(vv) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return append(json.RawMessage(nil), vv...), nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
}

func strPtr(v string) *string {
	s := strings.TrimSpace(v)
	if s == "" {
		return nil
	}
	return &s
}

func timePtr(t time.Time) *time.Time {
	tt := t.UTC()
	return &tt
}

func defaultIntPtr(v int) *int {
	return &v
}

func toolResultContentAsJSONString(raw json.RawMessage) json.RawMessage {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		trimmed = "{}"
	}
	b, err := json.Marshal(trimmed)
	if err != nil {
		return json.RawMessage(`"{}"`)
	}
	return b
}

func nullableStr(v string) *string {
	return strPtr(v)
}

func (s *Service) bootstrapBriefingContext(ctx context.Context, portfolioID uuid.UUID) (portfolioCtx, marketCtx json.RawMessage) {
	if s == nil || s.toolExecutor == nil {
		return nil, nil
	}
	boot, ok := s.toolExecutor.(BriefingBootstrapper)
	if !ok {
		return nil, nil
	}
	pCtx, mCtx, err := boot.BootstrapBriefingContext(ctx, portfolioID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("briefing_bootstrap_failed", zap.String("portfolio_id", portfolioID.String()), zap.Error(err))
		}
		return nil, nil
	}
	return pCtx, mCtx
}

func enrichBriefingUserInput(req RunBriefingRequest) (json.RawMessage, error) {
	payload := map[string]any{}
	if len(bytes.TrimSpace(req.UserInput)) > 0 {
		_ = json.Unmarshal(req.UserInput, &payload)
	}
	if _, ok := payload["portfolio_id"]; !ok {
		payload["portfolio_id"] = req.PortfolioID.String()
	}
	if _, ok := payload["trigger_source"]; !ok {
		payload["trigger_source"] = strings.TrimSpace(req.TriggerSource)
	}
	if _, ok := payload["run_date"]; !ok {
		payload["run_date"] = req.RunDate.UTC().Format("2006-01-02")
	}
	if req.RequestedByUserID != nil {
		if _, ok := payload["requested_by_user_id"]; !ok {
			payload["requested_by_user_id"] = req.RequestedByUserID.String()
		}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func estimateCostUSD(inputTokens, outputTokens *int) *decimal.Decimal {
	// Conservative placeholder estimate for phase 1 auditability.
	if inputTokens == nil && outputTokens == nil {
		return nil
	}
	in := 0
	out := 0
	if inputTokens != nil {
		in = *inputTokens
	}
	if outputTokens != nil {
		out = *outputTokens
	}
	est := decimal.NewFromInt(int64(in)).Mul(decimal.RequireFromString("0.0000008")).
		Add(decimal.NewFromInt(int64(out)).Mul(decimal.RequireFromString("0.0000024")))
	return &est
}

func validationIssuesFromError(err error) []ValidationIssue {
	var ve *ValidationError
	if !errors.As(err, &ve) {
		return []ValidationIssue{{
			Code:   ValidationCodeMalformedJSON,
			Detail: err.Error(),
		}}
	}
	return ve.Issues
}

func applyTradeIdeaDefaults(out BriefingOutput, userInput json.RawMessage) BriefingOutput {
	settings := map[string]any{}
	if len(bytes.TrimSpace(userInput)) > 0 {
		_ = json.Unmarshal(userInput, &settings)
	}
	sizeDefault := defaultSizeFromSettings(settings)
	stopDefault := defaultStopFromSettings(settings)
	targetDefault := defaultTargetFromSettings(settings)
	for i := range out.TradeIdeas {
		if isUnknownLike(out.TradeIdeas[i].Size) {
			out.TradeIdeas[i].Size = sizeDefault
		}
		if isUnknownLike(out.TradeIdeas[i].Stop) {
			out.TradeIdeas[i].Stop = stopDefault
		}
		if isUnknownLike(out.TradeIdeas[i].Target) {
			out.TradeIdeas[i].Target = targetDefault
		}
	}
	return out
}

func defaultSizeFromSettings(settings map[string]any) string {
	if v, ok := asNumber(settings["risk_budget_per_trade_pct"]); ok && v > 0 {
		return fmt.Sprintf("Size to %.2f%% risk budget per idea", v)
	}
	return "Starter size with 0.50%-1.00% risk budget per idea"
}

func defaultStopFromSettings(settings map[string]any) string {
	style := strings.ToLower(strings.TrimSpace(anyToString(settings["stop_style"])))
	switch style {
	case "trailing":
		return "Use a trailing stop that tightens as price advances"
	case "thesis":
		return "Exit if thesis is invalidated by new information"
	case "hard", "hard price", "price":
		return "Use a hard stop at a clearly defined invalidation price"
	default:
		return "Use a hard invalidation stop before entry"
	}
}

func defaultTargetFromSettings(settings map[string]any) string {
	style := strings.ToLower(strings.TrimSpace(anyToString(settings["target_style"])))
	switch style {
	case "r-multiple", "r multiple", "r":
		return "Take profits near 2R and reassess remainder"
	case "weight target":
		return "Target portfolio weight toward configured concentration limits"
	case "% move", "percent", "percentage":
		return "Take profits at staged percentage move milestones"
	default:
		return "Take partial profits at predefined levels and trail remainder"
	}
}

func asNumber(v any) (float64, bool) {
	switch vv := v.(type) {
	case float64:
		return vv, true
	case float32:
		return float64(vv), true
	case int:
		return float64(vv), true
	case int64:
		return float64(vv), true
	case json.Number:
		n, err := vv.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func isUnknownLike(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return s == "" || s == "unknown" || s == "n/a" || s == "na" || s == "tbd" || s == "-"
}

// findActiveScheduledSession returns a scheduled session for this portfolio that is
// currently in-flight (queued or running) and was started recently enough to plausibly
// still be running. Stale sessions older than `staleAfter` are ignored so a single
// crashed/orphaned row doesn't permanently suppress future scheduled ticks.
//
// Only scheduled-trigger sessions can suppress a new scheduled tick; in-flight manual
// briefings do NOT block scheduled runs.
func (s *Service) findActiveScheduledSession(ctx context.Context, portfolioID uuid.UUID, now time.Time) (events.AgentSession, bool, error) {
	list, err := s.store.ListAgentSessionsForPortfolio(ctx, portfolioID, events.AgentSessionListFilter{
		Limit:  50,
		Offset: 0,
	})
	if err != nil {
		return events.AgentSession{}, false, err
	}
	// Anything older than 2x the session timeout is almost certainly a crash leftover.
	staleAfter := s.sessionTimeout * 2
	if staleAfter < 10*time.Minute {
		staleAfter = 10 * time.Minute
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.Add(-staleAfter)
	for _, item := range list {
		st := strings.TrimSpace(item.Status)
		if st != "queued" && st != "running" {
			continue
		}
		if strings.TrimSpace(item.TriggerSource) != "scheduled" {
			continue
		}
		started := item.CreatedAt
		if item.StartedAt != nil && !item.StartedAt.IsZero() {
			started = *item.StartedAt
		}
		if started.Before(cutoff) {
			// Stale row from a previous crash; do not let it block new scheduled ticks.
			s.log.Warn("agent_session_stale_active_ignored",
				zap.String("session_id", item.SessionID.String()),
				zap.String("portfolio_id", portfolioID.String()),
				zap.String("status", st),
				zap.Time("created_at", item.CreatedAt),
				zap.Time("cutoff", cutoff),
			)
			continue
		}
		return item, true, nil
	}
	return events.AgentSession{}, false, nil
}
