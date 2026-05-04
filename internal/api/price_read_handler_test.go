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
	r.GET("/v1/prices", listPricesHandler(store, zap.NewNop(), 5*time.Minute, time.Minute, nil))

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
	if body.StaleAfterSeconds != PriceListStaleAfter(time.Minute).Seconds() {
		t.Fatalf("staleness_threshold_seconds got %v want %v", body.StaleAfterSeconds, PriceListStaleAfter(time.Minute).Seconds())
	}
}

func TestPriceListStaleAfter(t *testing.T) {
	t.Parallel()
	if got := PriceListStaleAfter(0); got != 15*time.Minute {
		t.Fatalf("poll 0: got %v", got)
	}
	if got := PriceListStaleAfter(time.Minute); got != 5*time.Minute {
		t.Fatalf("poll 1m: got %v want 5m", got)
	}
	if got := PriceListStaleAfter(900 * time.Second); got != 75*time.Minute {
		t.Fatalf("poll 900s: got %v want 75m", got)
	}
}

func TestProviderDataStatus_duplicateTickRescue(t *testing.T) {
	t.Parallel()
	poll := 900 * time.Second
	staleSec := PriceListStaleAfter(poll).Seconds()
	rt := pricefeed.NewRuntimeTracker()
	rt.OnTickSuccess(time.Now().UTC(), "alpaca", false, 0)

	slightlyPast := staleSec + 50
	if providerDataStatus(staleSec, slightlyPast, rt, poll) != "fresh" {
		t.Fatalf("expected fresh when barely past threshold and fetch ok")
	}
	tooFarPast := staleSec + float64((2*poll).Seconds()) + 10
	if providerDataStatus(staleSec, tooFarPast, rt, poll) != "stale" {
		t.Fatalf("expected stale when projection age exceeds rescue band")
	}
	if providerDataStatus(staleSec, slightlyPast, nil, poll) != "stale" {
		t.Fatalf("expected stale without runtime tracker")
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
