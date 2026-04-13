package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

type stubInsightsService struct {
	out *InsightsExplainResponse
	err error

	explainCalls int
	lastReq      *InsightsExplainRequest
}

func (s *stubInsightsService) Explain(ctx context.Context, req InsightsExplainRequest) (*InsightsExplainResponse, error) {
	s.explainCalls++
	s.lastReq = &req
	if s.err != nil {
		return nil, s.err
	}
	return s.out, nil
}

func minimalAssemblerInput() portfolio.PortfolioAssemblerInput {
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	eid := uuid.New()
	return portfolio.PortfolioAssemblerInput{
		Positions: []portfolio.ProjectionRow{
			{
				Symbol:      "AAPL",
				Quantity:    decimal.NewFromInt(1),
				AverageCost: decimal.NewFromInt(100),
				RealizedPnL: decimal.Zero,
			},
		},
		PriceBySymbol: map[string]portfolio.PriceMarkInput{
			"AAPL": {
				Price:          decimal.NewFromInt(100),
				AsOfEventID:    eid,
				AsOfEventTime:  t0,
				ProcessingTime: t0,
			},
		},
		TradeApply: &portfolio.ApplyCursorMeta{EventID: eid, EventTime: t0, ProcessingTime: t0},
	}
}

// riskReadyStore wraps minimalAssemblerInput with return counts and sigmas so BuildRiskHTTPResponse succeeds.
func riskReadyStore(found bool) *fakeRiskReadStore {
	return &fakeRiskReadStore{
		found:  found,
		input:  minimalAssemblerInput(),
		counts: map[string]int{"AAPL": 5},
		sigmas: map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("0.02")},
	}
}

func TestPostInsightsExplain_openAINotConfigured_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440077")
	store := riskReadyStore(true)
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         store,
		RiskRead:              store,
		RiskSigmaWindowN:      60,
		Insights:              nil,
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/insights/explain", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(RequestIDHeader, fixedRequestID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeInsufficientData, "AI insights are not configured for this deployment", fixedRequestID)

	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Details["reason"] != InsightsReasonOpenAINotConfigured {
		t.Fatalf("details.reason = %v want %q", env.Details["reason"], InsightsReasonOpenAINotConfigured)
	}
}

func TestPostInsightsExplain_notFound_beforeProviderCheck(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440078")
	store := riskReadyStore(false)
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         store,
		RiskRead:              store,
		RiskSigmaWindowN:      60,
		Insights:              nil,
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/insights/explain", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeNotFound, "portfolio not found", fixedRequestID)
}

func TestPostInsightsExplain_stubService_success(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440079")
	store := riskReadyStore(true)
	tradePayload, _ := json.Marshal(domain.TradePayload{
		TradeID: "t1", Symbol: "AAPL", Side: domain.SideBuy,
		Quantity: decimal.NewFromInt(1), Price: decimal.NewFromInt(100), Currency: "USD",
	})
	store.recentEvents = []domain.EventEnvelope{
		{
			EventID:   uuid.New(),
			EventType: domain.EventTypeTradeExecuted,
			EventTime: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			Source:    "test",
			Payload:   tradePayload,
		},
	}
	stub := &stubInsightsService{out: &InsightsExplainResponse{Explanation: "ok", Model: "test-model"}}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         store,
		RiskRead:              store,
		RiskSigmaWindowN:      60,
		Insights:              stub,
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/insights/explain", bytes.NewReader([]byte(`{"focus":"risk"}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if stub.explainCalls != 1 {
		t.Fatalf("Explain calls = %d want 1", stub.explainCalls)
	}
	if stub.lastReq == nil || stub.lastReq.Context == nil {
		t.Fatal("expected Context on Explain request")
	}
	if stub.lastReq.Context.Risk.PortfolioID != pid.String() {
		t.Fatalf("context risk portfolio_id = %q", stub.lastReq.Context.Risk.PortfolioID)
	}
	if stub.lastReq.Context.Risk.Var95_1d == "" {
		t.Fatal("expected grounded risk in context")
	}
	if len(stub.lastReq.Context.RecentEvents) != 1 {
		t.Fatalf("recent events = %d", len(stub.lastReq.Context.RecentEvents))
	}
	if stub.lastReq.Context.RecentEvents[0].Summary["symbol"] != "AAPL" {
		t.Fatalf("redacted summary = %#v", stub.lastReq.Context.RecentEvents[0].Summary)
	}

	var got InsightsExplainResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Explanation != "ok" || got.Model != "test-model" {
		t.Fatalf("response = %#v", got)
	}
	if got.Context == nil || got.Context.PortfolioID != pid.String() {
		t.Fatalf("response context = %#v", got.Context)
	}
}

func TestPostInsightsExplain_unpricedOpen_noExplainCall(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440080")
	store := &fakeRiskReadStore{
		found: true,
		input: portfolio.PortfolioAssemblerInput{
			Positions: []portfolio.ProjectionRow{
				{Symbol: "AAPL", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(100), RealizedPnL: decimal.Zero},
			},
			PriceBySymbol: map[string]portfolio.PriceMarkInput{},
		},
	}
	stub := &stubInsightsService{out: &InsightsExplainResponse{Explanation: "should-not-run"}}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         store,
		RiskRead:              store,
		RiskSigmaWindowN:      60,
		Insights:              stub,
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/insights/explain", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(RequestIDHeader, fixedRequestID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if stub.explainCalls != 0 {
		t.Fatalf("Explain should not run; calls=%d", stub.explainCalls)
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != ErrCodeInsufficientData {
		t.Fatalf("error_code=%q", env.ErrorCode)
	}
	if env.Details["reason"] != InsightsReasonUnpricedOpenPositions {
		t.Fatalf("details.reason=%v", env.Details["reason"])
	}
}

func TestPostInsightsExplain_insufficientReturns_noExplainCall(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440081")
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &fakeRiskReadStore{
		found: true,
		input: portfolio.PortfolioAssemblerInput{
			Positions: []portfolio.ProjectionRow{
				{Symbol: "AAPL", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(100), RealizedPnL: decimal.Zero},
			},
			PriceBySymbol: map[string]portfolio.PriceMarkInput{
				"AAPL": {
					Price:          decimal.NewFromInt(100),
					AsOfEventID:    uuid.New(),
					AsOfEventTime:  t0,
					ProcessingTime: t0,
				},
			},
			TradeApply: &portfolio.ApplyCursorMeta{EventID: uuid.New(), EventTime: t0, ProcessingTime: t0},
		},
		counts: map[string]int{"AAPL": 1},
	}
	stub := &stubInsightsService{out: &InsightsExplainResponse{Explanation: "no"}}
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         store,
		RiskRead:              store,
		RiskSigmaWindowN:      60,
		Insights:              stub,
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/insights/explain", bytes.NewReader([]byte(`{}`)))
	req.Header.Set(RequestIDHeader, fixedRequestID)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if stub.explainCalls != 0 {
		t.Fatalf("Explain should not run; calls=%d", stub.explainCalls)
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Details["reason"] != InsightsReasonInsufficientReturnHistory {
		t.Fatalf("details.reason=%v", env.Details["reason"])
	}
}
