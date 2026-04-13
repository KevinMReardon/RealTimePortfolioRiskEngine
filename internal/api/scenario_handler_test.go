package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

func TestPostScenario_ValidationAndSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440066")
	t0 := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	baseStore := &fakeRiskReadStore{
		found: true,
		input: portfolio.PortfolioAssemblerInput{
			Positions: []portfolio.ProjectionRow{
				{
					Symbol:      "AAPL",
					Quantity:    decimal.NewFromInt(10),
					AverageCost: decimal.NewFromInt(100),
					RealizedPnL: decimal.Zero,
				},
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
	}

	t.Run("invalid_uuid_400", func(t *testing.T) {
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(baseStore, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[{"symbol":"AAPL","type":"PCT","value":0.1}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/not-a-uuid/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}
		var env APIError
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		if env.ErrorCode != ErrCodeValidation {
			t.Fatalf("error_code=%s", env.ErrorCode)
		}
	})

	t.Run("reserved_price_shard_400", func(t *testing.T) {
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(baseStore, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[{"symbol":"AAPL","type":"PCT","value":0.1}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+testPricePartitions[0].String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid_shock_type_400", func(t *testing.T) {
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(baseStore, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[{"symbol":"AAPL","type":"ABS","value":0.1}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty_shocks_400", func(t *testing.T) {
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(baseStore, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing_base_mark_422", func(t *testing.T) {
		store := &fakeRiskReadStore{
			found: true,
			input: portfolio.PortfolioAssemblerInput{
				Positions: []portfolio.ProjectionRow{
					{Symbol: "MSFT", Quantity: decimal.NewFromInt(1), AverageCost: decimal.NewFromInt(100), RealizedPnL: decimal.Zero},
				},
				PriceBySymbol: map[string]portfolio.PriceMarkInput{},
			},
		}
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(store, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[{"symbol":"MSFT","type":"PCT","value":0.1}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("ok_200_applies_pct_shock", func(t *testing.T) {
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(baseStore, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[{"symbol":"AAPL","type":"PCT","value":0.1}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}

		var resp scenarioHTTPResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.PortfolioID != pid.String() {
			t.Fatalf("portfolio_id=%s", resp.PortfolioID)
		}
		if resp.Base.Totals.MarketValue != "1000" {
			t.Fatalf("base mv=%s", resp.Base.Totals.MarketValue)
		}
		if resp.Shocked.Totals.MarketValue != "1100" {
			t.Fatalf("shocked mv=%s", resp.Shocked.Totals.MarketValue)
		}
		if resp.Delta.MarketValue != "100" || resp.Delta.UnrealizedPnL != "100" {
			t.Fatalf("delta=%+v", resp.Delta)
		}
		if len(resp.Shocks) != 1 || resp.Shocks[0].Type != "PCT" {
			t.Fatalf("shocks=%+v", resp.Shocks)
		}
		if len(resp.BaseMetadata.DrivingEventIDs) != len(resp.Base.DrivingEventIDs) {
			t.Fatalf("base_metadata driving ids len %d vs base %d", len(resp.BaseMetadata.DrivingEventIDs), len(resp.Base.DrivingEventIDs))
		}
		for i := range resp.Base.DrivingEventIDs {
			if resp.BaseMetadata.DrivingEventIDs[i] != resp.Base.DrivingEventIDs[i] {
				t.Fatalf("base_metadata driving_event_ids mismatch at %d", i)
			}
		}
		if (resp.BaseMetadata.AsOfEventID == nil) != (resp.Base.AsOfEventID == nil) {
			t.Fatalf("base_metadata as_of_event_id presence mismatch")
		}
		if resp.Base.AsOfEventID != nil && *resp.BaseMetadata.AsOfEventID != *resp.Base.AsOfEventID {
			t.Fatalf("base_metadata as_of_event_id mismatch")
		}
	})

	t.Run("ok_200_accepts_kind_alias", func(t *testing.T) {
		r := gin.New()
		r.POST("/v1/portfolios/:id/scenarios", postScenarioHandler(baseStore, zap.NewNop(), testPricePartitions))

		body := []byte(`{"shocks":[{"symbol":"AAPL","kind":"PCT","value":0.1}]}`)
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
		}

		var resp scenarioHTTPResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Shocks) != 1 || resp.Shocks[0].Type != "PCT" {
			t.Fatalf("shocks=%+v", resp.Shocks)
		}
	})
}
