package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion/pricefeed"
)

type fakePriceMarks struct {
	list   events.ListPriceMarksResult
	detail *events.PriceSymbolDetail
	found  bool
	err    error
}

func (f *fakePriceMarks) ListPriceMarks(ctx context.Context, p events.ListPriceMarksParams) (events.ListPriceMarksResult, error) {
	if f.err != nil {
		return events.ListPriceMarksResult{}, f.err
	}
	_ = p
	return f.list, nil
}

func (f *fakePriceMarks) GetPriceSymbolDetail(ctx context.Context, symbol string, historyLimit int) (*events.PriceSymbolDetail, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	_ = ctx
	_ = symbol
	_ = historyLimit
	return f.detail, f.found, nil
}

func TestListPrices_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	cp := "0.0123"
	store := &fakePriceMarks{
		list: events.ListPriceMarksResult{
			Total: 1,
			Items: []events.PriceMarkListRow{{
				Symbol: "AAPL", Price: "190.12", AsOf: now, UpdatedAt: now, Source: "pricefeed:twelvedata", ChangePct: &cp,
			}},
		},
	}
	r := gin.New()
	r.GET("/v1/prices", listPricesHandler(store, zap.NewNop(), 5*time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/prices?limit=10&offset=0&sort=symbol&order=asc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body listPricesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("unexpected body %+v", body)
	}
	if body.Items[0].Symbol != "AAPL" || body.Items[0].ProviderDataStatus != "fresh" {
		t.Fatalf("row: %+v", body.Items[0])
	}
}

func TestGetPriceFeedStatus_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	rt := pricefeed.NewRuntimeTracker()
	rt.OnTickStart(time.Now().UTC().Add(-time.Second))
	rt.OnTickSuccess(time.Now().UTC(), "twelvedata", true, 3)
	watchlist := &stubWatchlistManager{symbols: []string{"AAPL", "MSFT"}}

	r := gin.New()
	r.GET("/v1/price-feed/status", getPriceFeedStatusHandler(rt, true, "twelvedata", time.Minute, watchlist))

	req := httptest.NewRequest(http.MethodGet, "/v1/price-feed/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body priceFeedStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !body.FeedEnabled || body.WatchlistCount != 2 || !body.LastTickUsedFailover {
		t.Fatalf("unexpected %+v", body)
	}
}

type stubWatchlistManager struct {
	symbols []string
}

func (s *stubWatchlistManager) Watchlist() []string {
	return append([]string(nil), s.symbols...)
}

func (s *stubWatchlistManager) SetWatchlist(symbols []string) {
	s.symbols = append([]string(nil), symbols...)
}

func TestPutPriceFeedWatchlist_contract(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	watchlist := &stubWatchlistManager{symbols: []string{"AAPL"}}

	r := gin.New()
	r.PUT("/v1/price-feed/watchlist", putPriceFeedWatchlistHandler(watchlist))

	req := httptest.NewRequest(http.MethodPut, "/v1/price-feed/watchlist", strings.NewReader(`{"watchlist":["msft","btc-usd","msft"]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body priceFeedWatchlistResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(body.Watchlist) != 3 {
		t.Fatalf("watchlist not returned, got %+v", body.Watchlist)
	}
}
