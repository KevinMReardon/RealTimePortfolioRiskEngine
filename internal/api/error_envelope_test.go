package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

func assertAPIErrorEnvelope(t *testing.T, body []byte, wantCode, wantMsg, wantRequestID string) {
	t.Helper()
	var got APIError
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal API error: %v body=%s", err, string(body))
	}
	if got.ErrorCode != wantCode {
		t.Errorf("error_code = %q want %q", got.ErrorCode, wantCode)
	}
	if got.Message != wantMsg {
		t.Errorf("message = %q want %q", got.Message, wantMsg)
	}
	if got.RequestID != wantRequestID {
		t.Errorf("request_id = %q want %q", got.RequestID, wantRequestID)
	}
	if got.Details == nil {
		t.Error("details must be non-nil (JSON object)")
	}
}

// fixedRequestID is used for stable "golden" JSON contracts in tests.
const fixedRequestID = "aaaaaaaa-bbbb-4ccc-bbbb-aaaaaaaaaaaa"

func TestAPIErrorEnvelope_TradeZeroQuantity_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop()
	r := testRouter(logger, ingestion.NewService(&fakeIngestStore{}))

	body := map[string]any{
		"portfolio_id":    uuid.MustParse("550e8400-e29b-41d4-a716-446655440000").String(),
		"idempotency_key": "k-contract-qty",
		"source":          "trade_api",
		"trade": map[string]any{
			"trade_id": "t1",
			"symbol":   "AAPL",
			"side":     "BUY",
			"quantity": "0",
			"price":    "100",
			"currency": "USD",
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeValidation, "quantity must be > 0", fixedRequestID)
}

func TestAPIErrorEnvelope_TradeInvalidJSON_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop()
	r := testRouter(logger, ingestion.NewService(&fakeIngestStore{}))

	req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader([]byte(`not-json`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeValidation, "invalid request body including JSON shape", fixedRequestID)
}

func TestAPIErrorEnvelope_PriceNegative_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop()
	r := testRouter(logger, ingestion.NewService(&fakeIngestStore{}))

	body := map[string]any{
		"idempotency_key": "px-bad",
		"source":          "price_feed",
		"price": map[string]any{
			"symbol":          "AAPL",
			"price":           "-1",
			"currency":        "USD",
			"source_sequence": 1,
		},
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/prices", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeValidation, "price must be > 0", fixedRequestID)
}

type fakePortfolioReadStore struct {
	input portfolio.PortfolioAssemblerInput
	found bool
	err   error
}

func (f *fakePortfolioReadStore) LoadPortfolioAssemblerInput(ctx context.Context, portfolioID uuid.UUID) (portfolio.PortfolioAssemblerInput, bool, error) {
	if f.err != nil {
		return portfolio.PortfolioAssemblerInput{}, false, f.err
	}
	return f.input, f.found, nil
}

func TestAPIErrorEnvelope_GetPortfolio_notFound_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	logger := zap.NewNop()
	r := NewRouter(RouterConfig{
		Logger:                logger,
		ReadPortfolio:         &fakePortfolioReadStore{found: false},
		PriceStreamPartitions: testPricePartitions,
	})
	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440099")
	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+pid.String(), nil)
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeNotFound, "portfolio not found", fixedRequestID)
}

func TestAPIErrorEnvelope_GetPortfolio_badUUID_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	r := NewRouter(RouterConfig{
		Logger:                zap.NewNop(),
		ReadPortfolio:         &fakePortfolioReadStore{found: true},
		PriceStreamPartitions: testPricePartitions,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/not-a-uuid", nil)
	req.Header.Set(RequestIDHeader, fixedRequestID)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	assertAPIErrorEnvelope(t, rec.Body.Bytes(), ErrCodeValidation, "portfolio id must be a UUID", fixedRequestID)
}
