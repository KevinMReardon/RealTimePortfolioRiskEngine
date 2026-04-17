package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

func apiIntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set INTEGRATION_DATABASE_URL or TEST_DATABASE_URL for API integration tests")
	}
	return dsn
}

func newAPIIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiIntegrationDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func integrationPricePartitions() []uuid.UUID {
	return config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 4)
}

func integrationRouter(pool *pgxpool.Pool, log *zap.Logger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	repo := events.NewPostgresStore(pool)
	svc := ingestion.NewService(repo)
	return NewRouter(RouterConfig{
		Logger:                log,
		Ingest:                svc,
		ReadPortfolio:         repo,
		RiskRead:              repo,
		RiskSigmaWindowN:      60,
		PriceStreamPartitions: integrationPricePartitions(),
		PriceMarksRead:        repo,
		PriceFeedEnabled:      false,
		PriceFeedProvider:     "twelvedata",
		PriceFeedPollInterval: time.Minute,
	})
}

func cleanupTradePortfolio(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM projection_cursor WHERE portfolio_id = $1`,
		`DELETE FROM positions_projection WHERE portfolio_id = $1`,
		`DELETE FROM dlq_events WHERE portfolio_id = $1`,
		`DELETE FROM events WHERE portfolio_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, pid); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
}

func postTradeBody(portfolio uuid.UUID, idem, tradeID string, qty string) []byte {
	body := map[string]any{
		"portfolio_id":    portfolio.String(),
		"idempotency_key": idem,
		"source":          "integration_test",
		"trade": map[string]any{
			"trade_id": tradeID,
			"symbol":   "AAPL",
			"side":     "BUY",
			"quantity": qty,
			"price":    "100",
			"currency": "USD",
		},
	}
	b, _ := json.Marshal(body)
	return b
}

func startTradeWorker(t *testing.T, pool *pgxpool.Pool, log *zap.Logger) context.CancelFunc {
	t.Helper()
	repo := events.NewPostgresStore(pool)
	w := events.NewWorker(repo, log, 30*time.Millisecond, 0, 0, 1, integrationPricePartitions())
	runCtx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = w.Run(runCtx)
	}()
	return cancel
}

func waitPositionQty(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID, symbol string, want decimal.Decimal, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var qstr string
		err := pool.QueryRow(ctx, `
			SELECT quantity::text FROM positions_projection WHERE portfolio_id = $1 AND symbol = $2
		`, pid, symbol).Scan(&qstr)
		if err == nil {
			q, err := decimal.NewFromString(qstr)
			if err == nil && q.Equal(want) {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	var final string
	_ = pool.QueryRow(ctx, `SELECT quantity::text FROM positions_projection WHERE portfolio_id = $1 AND symbol = $2`, pid, symbol).Scan(&final)
	t.Fatalf("timeout waiting for %s %s qty %s (last %q)", pid, symbol, want, final)
}

func TestHTTP_TradeDedupe_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool := newAPIIntegrationPool(t)
	pid := uuid.New()
	cleanupTradePortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupTradePortfolio(t, ctx, pool, pid) })

	log := zap.NewNop()
	router := integrationRouter(pool, log)

	body := postTradeBody(pid, "http-idem-1", "t-http", "5")

	req1 := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first status %d body=%s", rec1.Code, rec1.Body.String())
	}
	var r1 map[string]any
	if err := json.Unmarshal(rec1.Body.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	id1, _ := r1["event_id"].(string)

	req2 := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second status %d want 200 body=%s", rec2.Code, rec2.Body.String())
	}
	var r2 map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}
	if r2["status"] != "duplicate" {
		t.Fatalf("second status field: %v", r2["status"])
	}
	id2, _ := r2["event_id"].(string)
	if id2 != id1 {
		t.Fatalf("event_id mismatch: %q vs %q", id2, id1)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE portfolio_id = $1`, pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("event rows: got %d want 1", n)
	}

	cancel := startTradeWorker(t, pool, log)
	defer cancel()
	waitPositionQty(t, ctx, pool, pid, "AAPL", decimal.NewFromInt(5), 15*time.Second)
}

func TestHTTP_Concurrency_AppendsAndWorker(t *testing.T) {
	ctx := context.Background()
	pool := newAPIIntegrationPool(t)
	pid := uuid.New()
	cleanupTradePortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupTradePortfolio(t, ctx, pool, pid) })

	log := zap.NewNop()
	router := integrationRouter(pool, log)
	cancel := startTradeWorker(t, pool, log)
	defer cancel()

	const n = 24
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := postTradeBody(pid, uuid.New().String(), "c-trade", "1")
			req := httptest.NewRequest(http.MethodPost, "/v1/trades", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				errs <- fmt.Sprintf("post %d: status %d %s", i, rec.Code, rec.Body.String())
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Fatal(msg)
	}

	waitPositionQty(t, ctx, pool, pid, "AAPL", decimal.NewFromInt(n), 30*time.Second)

	var cur events.Cursor
	repo := events.NewPostgresStore(pool)
	cur, err := repo.LoadProjectionCursor(ctx, pid)
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	if cur.IsZero() {
		t.Fatal("expected non-zero projection_cursor after apply")
	}
}

func cleanupPriceShard(t *testing.T, ctx context.Context, pool *pgxpool.Pool, partition uuid.UUID, symbol string) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM projection_cursor WHERE portfolio_id = $1`,
		`DELETE FROM dlq_events WHERE portfolio_id = $1`,
		`DELETE FROM events WHERE portfolio_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, partition); err != nil {
			t.Fatalf("cleanup partition: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM prices_projection WHERE symbol = $1`, symbol); err != nil {
		t.Fatalf("cleanup prices_projection: %v", err)
	}
}

func postPriceBody(symbol, idem, price string) []byte {
	body := map[string]any{
		"idempotency_key": idem,
		"source":          "integration_test",
		"price": map[string]any{
			"symbol":          symbol,
			"price":           price,
			"currency":        "USD",
			"source_sequence": 1,
		},
	}
	b, _ := json.Marshal(body)
	return b
}

func startPriceWorker(t *testing.T, pool *pgxpool.Pool, log *zap.Logger) context.CancelFunc {
	t.Helper()
	repo := events.NewPostgresStore(pool)
	p := events.NewPricePool(repo, log, 30*time.Millisecond, 0, 0, 1, integrationPricePartitions())
	runCtx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = p.Run(runCtx)
	}()
	return cancel
}

func waitPriceProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, symbol string, want decimal.Decimal, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var pstr string
		err := pool.QueryRow(ctx, `
			SELECT price::text FROM prices_projection WHERE symbol = $1
		`, symbol).Scan(&pstr)
		if err == nil {
			p, err := decimal.NewFromString(pstr)
			if err == nil && p.Equal(want) {
				return
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	var last string
	_ = pool.QueryRow(ctx, `SELECT price::text FROM prices_projection WHERE symbol = $1`, symbol).Scan(&last)
	t.Fatalf("timeout waiting for prices_projection %s price %s (last %q)", symbol, want, last)
}

func TestHTTP_PriceUpdated_MaterializesProjection(t *testing.T) {
	ctx := context.Background()
	pool := newAPIIntegrationPool(t)
	partitions := integrationPricePartitions()
	const symbol = "ZZPHASE8PX"
	partition, err := config.PricePartitionForSymbol(partitions, symbol)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPriceShard(t, ctx, pool, partition, symbol)
	t.Cleanup(func() { cleanupPriceShard(t, ctx, pool, partition, symbol) })

	log := zap.NewNop()
	router := integrationRouter(pool, log)
	want := decimal.RequireFromString("199.50")

	body := postPriceBody(symbol, "px-phase8-1", want.String())
	req := httptest.NewRequest(http.MethodPost, "/v1/prices", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}

	cancel := startPriceWorker(t, pool, log)
	defer cancel()
	waitPriceProjection(t, ctx, pool, symbol, want, 15*time.Second)

	var cur events.Cursor
	repo := events.NewPostgresStore(pool)
	cur, err = repo.LoadProjectionCursor(ctx, partition)
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	if cur.IsZero() {
		t.Fatal("expected non-zero projection_cursor on price shard after apply")
	}
}

func cleanupPortfolioReadRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID, symbols []string) {
	t.Helper()
	for _, q := range []string{
		`DELETE FROM projection_cursor WHERE portfolio_id = $1`,
		`DELETE FROM positions_projection WHERE portfolio_id = $1`,
		`DELETE FROM dlq_events WHERE portfolio_id = $1`,
		`DELETE FROM events WHERE portfolio_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, pid); err != nil {
			t.Fatalf("cleanup: %v", err)
		}
	}
	for _, s := range symbols {
		_, _ = pool.Exec(ctx, `DELETE FROM prices_projection WHERE symbol = $1`, s)
	}
}

func TestHTTP_GetPortfolio_NotFound(t *testing.T) {
	pool := newAPIIntegrationPool(t)
	router := integrationRouter(pool, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_GetPortfolio_AssembledFromProjections(t *testing.T) {
	ctx := context.Background()
	pool := newAPIIntegrationPool(t)
	pid := uuid.New()
	symbol := "ZZGETPORTPX"
	evSeed := uuid.New()
	evCursor := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	evPrice := uuid.MustParse("10000000-0000-0000-0000-000000000002")
	tCursor := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	tPriceMark := time.Date(2026, 4, 7, 11, 0, 0, 0, time.UTC)
	pricePart := integrationPricePartitions()[0]

	cleanupPortfolioReadRows(t, ctx, pool, pid, []string{symbol})
	t.Cleanup(func() { cleanupPortfolioReadRows(t, ctx, pool, pid, []string{symbol}) })
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE event_id = ANY($1::uuid[])`, []uuid.UUID{evSeed, evCursor, evPrice}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE event_id = ANY($1::uuid[])`, []uuid.UUID{evSeed, evCursor, evPrice})
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, 'TradeExecuted', 'seed-get-1', 'integration_test', '{}'::jsonb)
	`, evSeed, pid, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, 'TradeExecuted', 'seed-get-cursor', 'integration_test', '{}'::jsonb)
	`, evCursor, pid, tCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, 'PriceUpdated', 'seed-get-px', 'integration_test', '{}'::jsonb)
	`, evPrice, pricePart, tPriceMark); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projection_cursor (portfolio_id, last_event_time, last_event_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (portfolio_id) DO UPDATE SET last_event_time = EXCLUDED.last_event_time, last_event_id = EXCLUDED.last_event_id, updated_at = NOW()
	`, pid, tCursor, evCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO positions_projection (portfolio_id, symbol, quantity, cost_basis, realized_pnl, updated_at)
		VALUES ($1, $2, 10, 50, 5, NOW())
	`, pid, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO prices_projection (symbol, price, as_of, updated_at, as_of_event_id)
		VALUES ($1, 60, $2, NOW(), $3)
		ON CONFLICT (symbol) DO UPDATE SET
			price = EXCLUDED.price,
			as_of = EXCLUDED.as_of,
			updated_at = NOW(),
			as_of_event_id = EXCLUDED.as_of_event_id
	`, symbol, tPriceMark, evPrice); err != nil {
		t.Fatal(err)
	}

	router := integrationRouter(pool, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+pid.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}

	var got portfolio.PortfolioView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.PortfolioID != pid.String() {
		t.Fatalf("portfolio_id %s", got.PortfolioID)
	}
	if len(got.Positions) != 1 || got.Positions[0].Symbol != symbol {
		t.Fatalf("positions %+v", got.Positions)
	}
	if got.Totals.MarketValue != "600" || got.Totals.RealizedPnL != "5" || got.Totals.UnrealizedPnL != "100" {
		t.Fatalf("totals %+v", got.Totals)
	}
	if len(got.UnpricedSymbols) != 0 {
		t.Fatalf("unpriced %+v", got.UnpricedSymbols)
	}
	if got.AsOfPositions == nil || got.AsOfPositions.EventID != evCursor {
		t.Fatalf("as_of_positions %+v want event %s", got.AsOfPositions, evCursor)
	}
	if !got.AsOfPositions.EventTime.Equal(tCursor) {
		t.Fatalf("as_of_positions.event_time got %v want %v", got.AsOfPositions.EventTime, tCursor)
	}
	if len(got.AsOfPrices) != 1 || got.AsOfPrices[0].EventID != evPrice || got.AsOfPrices[0].Symbol != symbol {
		t.Fatalf("as_of_prices %+v", got.AsOfPrices)
	}
	if !got.AsOfPrices[0].EventTime.Equal(tPriceMark) {
		t.Fatalf("as_of_prices[0].event_time got %v want %v", got.AsOfPrices[0].EventTime, tPriceMark)
	}
	if got.AsOfEventID != nil || got.AsOfEventTime != nil || got.AsOfProcessingTime != nil {
		t.Fatalf("expected no merged cross-partition as_of, got id=%v t=%v proc=%v", got.AsOfEventID, got.AsOfEventTime, got.AsOfProcessingTime)
	}
}

func TestHTTP_GetPortfolio_LineageSeparateUnderLag(t *testing.T) {
	ctx := context.Background()
	pool := newAPIIntegrationPool(t)
	pid := uuid.New()
	symbol := "ZZLAGPORTPX"
	evSeed := uuid.New()
	evPos := uuid.New()
	evPrice := uuid.New()
	tPos := time.Date(2026, 4, 7, 18, 0, 0, 0, time.UTC)
	tPrice := tPos.Add(-3 * time.Hour)
	pricePart := integrationPricePartitions()[1]

	cleanupPortfolioReadRows(t, ctx, pool, pid, []string{symbol})
	t.Cleanup(func() { cleanupPortfolioReadRows(t, ctx, pool, pid, []string{symbol}) })
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE event_id = ANY($1::uuid[])`, []uuid.UUID{evSeed, evPos, evPrice}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE event_id = ANY($1::uuid[])`, []uuid.UUID{evSeed, evPos, evPrice})
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, 'TradeExecuted', 'lag-seed', 'integration_test', '{}'::jsonb)
	`, evSeed, pid, tPos.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, 'TradeExecuted', 'lag-pos', 'integration_test', '{}'::jsonb)
	`, evPos, pid, tPos); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
		VALUES ($1, $2, $3, 'PriceUpdated', 'lag-px', 'integration_test', '{}'::jsonb)
	`, evPrice, pricePart, tPrice); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projection_cursor (portfolio_id, last_event_time, last_event_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (portfolio_id) DO UPDATE SET last_event_time = EXCLUDED.last_event_time, last_event_id = EXCLUDED.last_event_id, updated_at = NOW()
	`, pid, tPos, evPos); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO positions_projection (portfolio_id, symbol, quantity, cost_basis, realized_pnl, updated_at)
		VALUES ($1, $2, 3, 40, 0, NOW())
	`, pid, symbol); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO prices_projection (symbol, price, as_of, updated_at, as_of_event_id)
		VALUES ($1, 50, $2, NOW(), $3)
		ON CONFLICT (symbol) DO UPDATE SET
			price = EXCLUDED.price,
			as_of = EXCLUDED.as_of,
			updated_at = NOW(),
			as_of_event_id = EXCLUDED.as_of_event_id
	`, symbol, tPrice, evPrice); err != nil {
		t.Fatal(err)
	}

	router := integrationRouter(pool, zap.NewNop())
	req := httptest.NewRequest(http.MethodGet, "/v1/portfolios/"+pid.String(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"as_of_event_id", "as_of_event_time", "as_of_processing_time"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("response must not imply single snapshot (%s present)", key)
		}
	}

	var got portfolio.PortfolioView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AsOfPositions == nil || got.AsOfPositions.EventID != evPos {
		t.Fatalf("positions lineage %+v", got.AsOfPositions)
	}
	if !got.AsOfPositions.EventTime.Equal(tPos) {
		t.Fatalf("positions as_of time got %v want %v", got.AsOfPositions.EventTime, tPos)
	}
	if len(got.AsOfPrices) != 1 || got.AsOfPrices[0].EventID != evPrice {
		t.Fatalf("price lineage %+v", got.AsOfPrices)
	}
	if !got.AsOfPrices[0].EventTime.Equal(tPrice) {
		t.Fatalf("price as_of time got %v want %v (stale mark vs newer positions apply)", got.AsOfPrices[0].EventTime, tPrice)
	}
}

// V1 exit criterion: seeded portfolio + known mark, +10% shock on one leg, deterministic shocked
// market value (hand qty×price×1.1 summed with unshocked legs); repeat POST returns identical numbers.
func TestHTTP_Scenario_pctShockDeterministicRepeat(t *testing.T) {
	ctx := context.Background()
	pool := newAPIIntegrationPool(t)
	pid := uuid.New()
	symShocked := "ZZSCENA1"
	symOther := "ZZSCENB1"
	parts := integrationPricePartitions()
	partA, err := config.PricePartitionForSymbol(parts, symShocked)
	if err != nil {
		t.Fatal(err)
	}
	partB, err := config.PricePartitionForSymbol(parts, symOther)
	if err != nil {
		t.Fatal(err)
	}

	evSeed := uuid.New()
	evCursor := uuid.New()
	evPriceA := uuid.New()
	evPriceB := uuid.New()
	eventIDs := []uuid.UUID{evSeed, evCursor, evPriceA, evPriceB}
	tCursor := time.Date(2026, 4, 11, 16, 0, 0, 0, time.UTC)
	tPriceA := tCursor.Add(-2 * time.Hour)
	tPriceB := tCursor.Add(-3 * time.Hour)

	cleanupPortfolioReadRows(t, ctx, pool, pid, []string{symShocked, symOther})
	if _, err := pool.Exec(ctx, `DELETE FROM events WHERE event_id = ANY($1::uuid[])`, eventIDs); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupPortfolioReadRows(t, ctx, pool, pid, []string{symShocked, symOther})
		_, _ = pool.Exec(ctx, `DELETE FROM events WHERE event_id = ANY($1::uuid[])`, eventIDs)
	})

	for _, stmt := range []struct {
		q string
		a []any
	}{
		{`INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
			VALUES ($1, $2, $3, 'TradeExecuted', 'scen-seed', 'integration_test', '{}'::jsonb)`,
			[]any{evSeed, pid, time.Now().UTC()}},
		{`INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
			VALUES ($1, $2, $3, 'TradeExecuted', 'scen-cursor', 'integration_test', '{}'::jsonb)`,
			[]any{evCursor, pid, tCursor}},
		{`INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
			VALUES ($1, $2, $3, 'PriceUpdated', 'scen-px-a', 'integration_test', '{}'::jsonb)`,
			[]any{evPriceA, partA, tPriceA}},
		{`INSERT INTO events (event_id, portfolio_id, event_time, event_type, idempotency_key, source, payload)
			VALUES ($1, $2, $3, 'PriceUpdated', 'scen-px-b', 'integration_test', '{}'::jsonb)`,
			[]any{evPriceB, partB, tPriceB}},
	} {
		if _, err := pool.Exec(ctx, stmt.q, stmt.a...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projection_cursor (portfolio_id, last_event_time, last_event_id, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (portfolio_id) DO UPDATE SET last_event_time = EXCLUDED.last_event_time, last_event_id = EXCLUDED.last_event_id, updated_at = NOW()
	`, pid, tCursor, evCursor); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO positions_projection (portfolio_id, symbol, quantity, cost_basis, realized_pnl, updated_at)
		VALUES ($1, $2, 10, 100, 0, NOW())
	`, pid, symShocked); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO positions_projection (portfolio_id, symbol, quantity, cost_basis, realized_pnl, updated_at)
		VALUES ($1, $2, 5, 40, 0, NOW())
	`, pid, symOther); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO prices_projection (symbol, price, as_of, updated_at, as_of_event_id)
		VALUES ($1, 100, $2, NOW(), $3)
		ON CONFLICT (symbol) DO UPDATE SET
			price = EXCLUDED.price,
			as_of = EXCLUDED.as_of,
			updated_at = NOW(),
			as_of_event_id = EXCLUDED.as_of_event_id
	`, symShocked, tPriceA, evPriceA); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO prices_projection (symbol, price, as_of, updated_at, as_of_event_id)
		VALUES ($1, 50, $2, NOW(), $3)
		ON CONFLICT (symbol) DO UPDATE SET
			price = EXCLUDED.price,
			as_of = EXCLUDED.as_of,
			updated_at = NOW(),
			as_of_event_id = EXCLUDED.as_of_event_id
	`, symOther, tPriceB, evPriceB); err != nil {
		t.Fatal(err)
	}

	qtyA := decimal.RequireFromString("10")
	priceA := decimal.RequireFromString("100")
	qtyB := decimal.RequireFromString("5")
	priceB := decimal.RequireFromString("50")
	wantBaseMV := qtyA.Mul(priceA).Add(qtyB.Mul(priceB))
	shockedAPrice := priceA.Mul(decimal.RequireFromString("1.1"))
	wantShockedMV := qtyA.Mul(shockedAPrice).Add(qtyB.Mul(priceB))
	wantDeltaMV := wantShockedMV.Sub(wantBaseMV)

	router := integrationRouter(pool, zap.NewNop())
	body := []byte(fmt.Sprintf(`{"shocks":[{"symbol":%q,"type":"PCT","value":0.1}]}`, symShocked))

	runOnce := func() scenarioHTTPResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/portfolios/"+pid.String()+"/scenarios", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("scenario status %d body=%s", rec.Code, rec.Body.String())
		}
		var resp scenarioHTTPResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}

	first := runOnce()
	second := runOnce()

	for i, resp := range []scenarioHTTPResponse{first, second} {
		gotBaseMV, err := decimal.NewFromString(resp.Base.Totals.MarketValue)
		if err != nil {
			t.Fatalf("run %d parse base mv: %v", i, err)
		}
		if !gotBaseMV.Equal(wantBaseMV) {
			t.Fatalf("run %d base market_value got %s want %s", i, gotBaseMV, wantBaseMV)
		}
		gotShockMV, err := decimal.NewFromString(resp.Shocked.Totals.MarketValue)
		if err != nil {
			t.Fatalf("run %d parse shocked mv: %v", i, err)
		}
		if !gotShockMV.Equal(wantShockedMV) {
			t.Fatalf("run %d shocked market_value got %s want %s (qty×price×1.1 on %s + other leg)", i, gotShockMV, wantShockedMV, symShocked)
		}
		gotDeltaMV, err := decimal.NewFromString(resp.Delta.MarketValue)
		if err != nil {
			t.Fatalf("run %d parse delta mv: %v", i, err)
		}
		if !gotDeltaMV.Equal(wantDeltaMV) {
			t.Fatalf("run %d delta market_value got %s want %s", i, gotDeltaMV, wantDeltaMV)
		}
	}

	if first.Shocked.Totals.MarketValue != second.Shocked.Totals.MarketValue {
		t.Fatalf("repeat mismatch shocked mv %q vs %q", first.Shocked.Totals.MarketValue, second.Shocked.Totals.MarketValue)
	}
	if first.Base.Totals.MarketValue != second.Base.Totals.MarketValue {
		t.Fatalf("repeat mismatch base mv %q vs %q", first.Base.Totals.MarketValue, second.Base.Totals.MarketValue)
	}
}
