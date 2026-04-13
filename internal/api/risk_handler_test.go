package api

import (
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

type fakeRiskReadStore struct {
	input   portfolio.PortfolioAssemblerInput
	found   bool
	errLoad error

	counts    map[string]int
	errCounts error
	sigmas    map[string]decimal.Decimal
	errSigmas error

	recentEvents    []domain.EventEnvelope
	errRecentEvents error
}

func (f *fakeRiskReadStore) LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error) {
	if f.errLoad != nil {
		return portfolio.PortfolioAssemblerInput{}, false, f.errLoad
	}
	in := f.input
	in.PortfolioID = portfolioID
	return in, f.found, nil
}

func (f *fakeRiskReadStore) LoadSymbolReturnSampleCounts(ctx context.Context, symbols []string, windowN int) (map[string]int, error) {
	if f.errCounts != nil {
		return nil, f.errCounts
	}
	if f.counts == nil {
		return map[string]int{}, nil
	}
	return f.counts, nil
}

func (f *fakeRiskReadStore) LoadSymbolSigma1D(ctx context.Context, symbols []string, windowN int) (map[string]decimal.Decimal, error) {
	if f.errSigmas != nil {
		return nil, f.errSigmas
	}
	if f.sigmas == nil {
		return map[string]decimal.Decimal{}, nil
	}
	return f.sigmas, nil
}

func (f *fakeRiskReadStore) ListRecentEventsForPortfolio(ctx context.Context, portfolioID uuid.UUID, limit int) ([]domain.EventEnvelope, error) {
	if f.errRecentEvents != nil {
		return nil, f.errRecentEvents
	}
	if f.recentEvents == nil {
		return []domain.EventEnvelope{}, nil
	}
	return append([]domain.EventEnvelope(nil), f.recentEvents...), nil
}

func TestGetRisk_UnpricedOpenPosition_409(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440099")
	store := &fakeRiskReadStore{
		found: true,
		input: portfolio.PortfolioAssemblerInput{
			Positions: []portfolio.ProjectionRow{
				{Symbol: "AAPL", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(100), RealizedPnL: decimal.Zero},
			},
			PriceBySymbol: map[string]portfolio.PriceMarkInput{},
		},
	}

	r := gin.New()
	r.GET("/v1/portfolios/:id/risk", getRiskHandler(store, zap.NewNop(), testPricePartitions, 60))

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+pid.String()+"/risk", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d want %d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != ErrCodeUnpricedPositionsPresent {
		t.Fatalf("error_code = %q", env.ErrorCode)
	}
}

func TestGetRisk_InsufficientHistory_422(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440088")
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

	r := gin.New()
	r.GET("/v1/portfolios/:id/risk", getRiskHandler(store, zap.NewNop(), testPricePartitions, 60))

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+pid.String()+"/risk", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d want %d body=%s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != ErrCodeInsufficientData {
		t.Fatalf("error_code = %q", env.ErrorCode)
	}
}

func TestGetRisk_OK_200(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440077")
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
		counts: map[string]int{"AAPL": 5},
		sigmas: map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("0.02")},
	}

	r := gin.New()
	r.GET("/v1/portfolios/:id/risk", getRiskHandler(store, zap.NewNop(), testPricePartitions, 60))

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+pid.String()+"/risk", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body RiskHTTPResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.PortfolioID != pid.String() {
		t.Fatalf("portfolio_id = %q", body.PortfolioID)
	}
	if len(body.Exposure) != 1 || body.Exposure[0].Symbol != "AAPL" {
		t.Fatalf("exposure = %+v", body.Exposure)
	}
	if body.Var95_1d == "" {
		t.Fatal("expected var_95_1d")
	}
	if body.Volatility.Sigma1DPortfolio == "" {
		t.Fatal("expected sigma_1d_portfolio")
	}
}
