package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set INTEGRATION_DATABASE_URL or TEST_DATABASE_URL to run integration tests (DB must have migrations applied)")
	}
	return dsn
}

func newIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, integrationDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func cleanupPortfolio(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(ctx, `DELETE FROM projection_cursor WHERE portfolio_id = $1`, pid)
	if err != nil {
		t.Fatalf("cleanup projection_cursor: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM positions_projection WHERE portfolio_id = $1`, pid)
	if err != nil {
		t.Fatalf("cleanup positions_projection: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM dlq_events WHERE portfolio_id = $1`, pid)
	if err != nil {
		t.Fatalf("cleanup dlq_events: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM events WHERE portfolio_id = $1`, pid)
	if err != nil {
		t.Fatalf("cleanup events: %v", err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM portfolio_snapshots WHERE portfolio_id = $1`, pid)
	if err != nil {
		t.Fatalf("cleanup portfolio_snapshots: %v", err)
	}
}

func tradeEnvelope(pid uuid.UUID, idem string, eventTime time.Time, tradeID string, qty int64) domain.EventEnvelope {
	p, _ := json.Marshal(domain.TradePayload{
		TradeID:  tradeID,
		Symbol:   "AAPL",
		Side:     domain.SideBuy,
		Quantity: decimal.NewFromInt(qty),
		Price:    decimal.NewFromInt(100),
		Currency: "USD",
	})
	return domain.EventEnvelope{
		EventID:        uuid.New(),
		EventType:      domain.EventTypeTradeExecuted,
		EventTime:      eventTime.UTC(),
		ProcessingTime: time.Now().UTC(),
		Source:         "integration_test",
		PortfolioID:    pid.String(),
		IdempotencyKey: idem,
		Payload:        p,
	}
}

func positionQty(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID, symbol string) decimal.Decimal {
	t.Helper()
	var qstr string
	err := pool.QueryRow(ctx, `
		SELECT quantity::text FROM positions_projection WHERE portfolio_id = $1 AND symbol = $2
	`, pid, symbol).Scan(&qstr)
	if err != nil {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(qstr)
	if err != nil {
		t.Fatalf("parse quantity: %v", err)
	}
	return d
}

func loadCursor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID) Cursor {
	t.Helper()
	repo := NewPostgresStore(pool)
	c, err := repo.LoadProjectionCursor(ctx, pid)
	if err != nil {
		t.Fatalf("LoadProjectionCursor: %v", err)
	}
	return c
}

func loadPositionLotProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID, symbol string) domain.PositionLot {
	t.Helper()
	var q, cb, rp string
	err := pool.QueryRow(ctx, `
		SELECT quantity::text, cost_basis::text, realized_pnl::text
		FROM positions_projection WHERE portfolio_id = $1 AND symbol = $2
	`, pid, symbol).Scan(&q, &cb, &rp)
	if err != nil {
		t.Fatalf("load positions_projection %s: %v", symbol, err)
	}
	qty, err := decimal.NewFromString(q)
	if err != nil {
		t.Fatalf("parse quantity: %v", err)
	}
	avg, err := decimal.NewFromString(cb)
	if err != nil {
		t.Fatalf("parse cost_basis: %v", err)
	}
	real, err := decimal.NewFromString(rp)
	if err != nil {
		t.Fatalf("parse realized_pnl: %v", err)
	}
	return domain.PositionLot{Quantity: qty, AverageCost: avg, RealizedPnL: real}
}

func assertPositionLotProjectionEqual(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID, symbol string, want domain.PositionLot) {
	t.Helper()
	got := loadPositionLotProjection(t, ctx, pool, pid, symbol)
	if !got.Quantity.Equal(want.Quantity) {
		t.Fatalf("quantity: got %s want %s", got.Quantity, want.Quantity)
	}
	if !got.AverageCost.Equal(want.AverageCost) {
		t.Fatalf("average_cost: got %s want %s", got.AverageCost, want.AverageCost)
	}
	if !got.RealizedPnL.Equal(want.RealizedPnL) {
		t.Fatalf("realized_pnl: got %s want %s", got.RealizedPnL, want.RealizedPnL)
	}
}

func partitionPumpUntilIdle(t *testing.T, ctx context.Context, pw *partitionWorker, pool *pgxpool.Pool, pid uuid.UUID, maxIters int) {
	t.Helper()
	for i := 0; i < maxIters; i++ {
		before := loadCursor(t, ctx, pool, pid)
		if err := pw.processIncremental(ctx); err != nil {
			t.Fatalf("processIncremental: %v", err)
		}
		after := loadCursor(t, ctx, pool, pid)
		if before.Time.Equal(after.Time) && before.ID == after.ID {
			return
		}
	}
	t.Fatalf("partitionPumpUntilIdle: not idle after %d iterations", maxIters)
}

// TestApply_Ordering_ShuffledInsert matches exit criteria §7 ordering: same portfolio,
// insert order shuffled; apply order follows (event_time, event_id); positions match canonical sorted apply.
func TestApply_Ordering_ShuffledInsert(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	pid := uuid.New()
	cleanupPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	repo := NewPostgresStore(pool)

	t0 := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	evA := tradeEnvelope(pid, "ord-a", t0, "t-a", 1)
	evB := tradeEnvelope(pid, "ord-b", t0.Add(time.Millisecond), "t-b", 2)
	evC := tradeEnvelope(pid, "ord-c", t0.Add(2*time.Millisecond), "t-c", 3)

	// Shuffle append order: C, A, B — DB replay order must still be A, B, C by event_time.
	for _, ev := range []domain.EventEnvelope{evC, evA, evB} {
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append %s: %v", ev.IdempotencyKey, err)
		}
	}

	want := portfolio.NewAggregate()
	pidStr := pid.String()
	for _, ev := range []domain.EventEnvelope{evA, evB, evC} {
		if err := want.ApplyEvent(pidStr, ev); err != nil {
			t.Fatalf("canonical apply: %v", err)
		}
	}
	wantQty := want.Quantity(pidStr, "AAPL")

	pw := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw.rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	gotQty := pw.agg.Quantity(pidStr, "AAPL")
	if !gotQty.Equal(wantQty) {
		t.Fatalf("positions after apply: got %s want %s (canonical sorted order)", gotQty, wantQty)
	}
}

// TestApply_Dedupe_SingleRow verifies same idempotency_key does not create a second event;
// one apply pass matches a single logical trade in positions.
func TestApply_Dedupe_SingleRow(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	pid := uuid.New()
	cleanupPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	repo := NewPostgresStore(pool)

	ev1 := tradeEnvelope(pid, "idem-same", time.Now().UTC(), "t1", 7)
	ev2 := ev1
	ev2.EventID = uuid.New()

	r1, err := repo.Append(ctx, ev1)
	if err != nil {
		t.Fatalf("first append: %v", err)
	}
	if !r1.Inserted {
		t.Fatal("first append should insert")
	}
	r2, err := repo.Append(ctx, ev2)
	if err != nil {
		t.Fatalf("second append: %v", err)
	}
	if r2.Inserted {
		t.Fatal("second ingest should be duplicate")
	}
	if r2.EventID != r1.EventID {
		t.Fatalf("canonical event_id: got %v want %v", r2.EventID, r1.EventID)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE portfolio_id = $1 AND idempotency_key = $2`, pid, "idem-same").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("events rows for idempotency key: got %d want 1", n)
	}

	pw := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw.rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	qty := positionQty(t, ctx, pool, pid, "AAPL")
	if !qty.Equal(decimal.NewFromInt(7)) {
		t.Fatalf("quantity after single logical trade: got %s want 7", qty)
	}
}

// TestApply_Restart_NoDoubleApply simulates process restart: cursor + projections durable;
// a fresh partitionWorker rebuilds from DB and incremental apply does not change state.
func TestApply_Restart_NoDoubleApply(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	pid := uuid.New()
	cleanupPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	repo := NewPostgresStore(pool)

	for i, idem := range []string{"r1", "r2", "r3"} {
		ev := tradeEnvelope(pid, idem, time.Now().UTC().Add(time.Duration(i)*time.Millisecond), "trade-"+idem, 1)
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append %s: %v", idem, err)
		}
	}

	pw1 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw1.rebuild(ctx); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	cur1 := loadCursor(t, ctx, pool, pid)
	qty1 := positionQty(t, ctx, pool, pid, "AAPL")
	if !qty1.Equal(decimal.NewFromInt(3)) {
		t.Fatalf("after first apply qty: got %s want 3", qty1)
	}

	pw2 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw2.rebuild(ctx); err != nil {
		t.Fatalf("restart rebuild: %v", err)
	}
	if err := pw2.processIncremental(ctx); err != nil {
		t.Fatalf("incremental after restart: %v", err)
	}

	cur2 := loadCursor(t, ctx, pool, pid)
	if cur2.Time != cur1.Time || cur2.ID != cur1.ID {
		t.Fatalf("cursor changed after no-op incremental: %+v -> %+v", cur1, cur2)
	}
	qty2 := positionQty(t, ctx, pool, pid, "AAPL")
	if !qty2.Equal(qty1) {
		t.Fatalf("quantity changed after restart: %s -> %s", qty1, qty2)
	}

	tail, err := repo.FetchAfter(ctx, pid, cur2, 50)
	if err != nil {
		t.Fatalf("FetchAfter: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("expected no tail after full apply, got %d events", len(tail))
	}
}

// TestApply_SameEventTime_TieBreakEventID ensures (event_time, event_id) ordering when times match.
func TestApply_SameEventTime_TieBreakEventID(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	pid := uuid.New()
	cleanupPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	repo := NewPostgresStore(pool)
	ts := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

	lowID := uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	highID := uuid.MustParse("00000000-0000-0000-0000-0000000000a2")

	evLater := tradeEnvelope(pid, "tie-b", ts, "tb", 1)
	evLater.EventID = highID
	evEarlier := tradeEnvelope(pid, "tie-a", ts, "ta", 10)
	evEarlier.EventID = lowID

	// Insert high event_id first — apply order must still be lowID then highID.
	if _, err := repo.Append(ctx, evLater); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Append(ctx, evEarlier); err != nil {
		t.Fatal(err)
	}

	want := portfolio.NewAggregate()
	pidStr := pid.String()
	if err := want.ApplyEvent(pidStr, evEarlier); err != nil {
		t.Fatal(err)
	}
	if err := want.ApplyEvent(pidStr, evLater); err != nil {
		t.Fatal(err)
	}
	wantQty := want.Quantity(pidStr, "AAPL")

	pw := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw.rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	got := pw.agg.Quantity(pidStr, "AAPL")
	if !got.Equal(wantQty) {
		t.Fatalf("qty got %s want %s (buy 10 then buy 1)", got, wantQty)
	}
}

func eventsPricePartitions() []uuid.UUID {
	return config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 4)
}

func cleanupPricePartition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, partition uuid.UUID, symbols []string) {
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
	for _, sym := range symbols {
		if _, err := pool.Exec(ctx, `DELETE FROM prices_projection WHERE symbol = $1`, sym); err != nil {
			t.Fatalf("cleanup prices_projection %s: %v", sym, err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM symbol_returns WHERE symbol = $1`, sym); err != nil {
			t.Fatalf("cleanup symbol_returns %s: %v", sym, err)
		}
	}
}

func priceUpdatedEnvelope(partition uuid.UUID, idem string, eventTime time.Time, symbol string, price decimal.Decimal) domain.EventEnvelope {
	payload, _ := json.Marshal(domain.PricePayload{
		Symbol:         symbol,
		Price:          price,
		Currency:       "USD",
		SourceSequence: 1,
	})
	return domain.EventEnvelope{
		EventID:        uuid.New(),
		EventType:      domain.EventTypePriceUpdated,
		EventTime:      eventTime.UTC(),
		ProcessingTime: time.Now().UTC(),
		Source:         "events_integration_test",
		PortfolioID:    partition.String(),
		IdempotencyKey: idem,
		Payload:        payload,
	}
}

func TestApply_AccountingAndMarkToMarket(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)

	const sym = "ZZACCTMTM"
	pid := uuid.New()
	parts := eventsPricePartitions()
	pricePart, err := config.PricePartitionForSymbol(parts, sym)
	if err != nil {
		t.Fatal(err)
	}

	cleanupPortfolio(t, ctx, pool, pid)
	cleanupPricePartition(t, ctx, pool, pricePart, []string{sym})
	t.Cleanup(func() {
		cleanupPortfolio(t, ctx, pool, pid)
		cleanupPricePartition(t, ctx, pool, pricePart, []string{sym})
	})

	repo := NewPostgresStore(pool)
	t0 := time.Date(2026, 4, 5, 15, 0, 0, 0, time.UTC)

	tr := func(idem string, ts time.Time, side domain.Side, qty int64, price int64) domain.EventEnvelope {
		p, _ := json.Marshal(domain.TradePayload{
			TradeID:  idem,
			Symbol:   sym,
			Side:     side,
			Quantity: decimal.NewFromInt(qty),
			Price:    decimal.NewFromInt(price),
			Currency: "USD",
		})
		return domain.EventEnvelope{
			EventID:        uuid.New(),
			EventType:      domain.EventTypeTradeExecuted,
			EventTime:      ts.UTC(),
			ProcessingTime: time.Now().UTC(),
			Source:         "events_integration_test",
			PortfolioID:    pid.String(),
			IdempotencyKey: idem,
			Payload:        p,
		}
	}

	for i, ev := range []domain.EventEnvelope{
		tr("acct-t1", t0, domain.SideBuy, 100, 10),
		tr("acct-t2", t0.Add(time.Millisecond), domain.SideBuy, 50, 12),
		tr("acct-t3", t0.Add(2*time.Millisecond), domain.SideSell, 30, 11),
	} {
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append trade %d: %v", i, err)
		}
	}

	pwTrade := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pwTrade.rebuild(ctx); err != nil {
		t.Fatalf("trade rebuild: %v", err)
	}

	wantAvg := decimal.NewFromInt(1600).Div(decimal.NewFromInt(150))
	var qStr, avgStr, realStr string
	err = pool.QueryRow(ctx, `
		SELECT quantity::text, cost_basis::text, realized_pnl::text
		FROM positions_projection WHERE portfolio_id = $1 AND symbol = $2
	`, pid, sym).Scan(&qStr, &avgStr, &realStr)
	if err != nil {
		t.Fatalf("positions_projection: %v", err)
	}
	qty, _ := decimal.NewFromString(qStr)
	avg, _ := decimal.NewFromString(avgStr)
	real, _ := decimal.NewFromString(realStr)
	if !qty.Equal(decimal.NewFromInt(120)) {
		t.Fatalf("quantity got %s want 120", qty)
	}
	if !avg.Equal(wantAvg.Round(8)) {
		t.Fatalf("average cost got %s want %s (8dp)", avg, wantAvg.Round(8))
	}
	wantRealFull := decimal.NewFromInt(11).Sub(wantAvg).Mul(decimal.NewFromInt(30))
	if !real.Equal(wantRealFull.Round(8)) {
		t.Fatalf("realized_pnl got %s want %s (8dp)", real, wantRealFull.Round(8))
	}

	px := priceUpdatedEnvelope(pricePart, "acct-px-1", t0.Add(3*time.Millisecond), sym, decimal.NewFromInt(13))
	if _, err := repo.Append(ctx, px); err != nil {
		t.Fatalf("append price: %v", err)
	}
	pwPrice := newPricePartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pricePart)
	if err := pwPrice.rebuild(ctx); err != nil {
		t.Fatalf("price rebuild: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT p.symbol, p.quantity::text, p.cost_basis::text, p.realized_pnl::text, pr.price::text
		FROM positions_projection p
		LEFT JOIN prices_projection pr ON pr.symbol = p.symbol
		WHERE p.portfolio_id = $1
	`, pid)
	if err != nil {
		t.Fatalf("join query: %v", err)
	}
	defer rows.Close()

	var mtm []portfolio.MarkToMarketRow
	for rows.Next() {
		var rowSym string
		var qs, as, rs string
		var pns sql.NullString
		if err := rows.Scan(&rowSym, &qs, &as, &rs, &pns); err != nil {
			t.Fatalf("scan: %v", err)
		}
		q, _ := decimal.NewFromString(qs)
		a, _ := decimal.NewFromString(as)
		r, _ := decimal.NewFromString(rs)
		row := portfolio.ProjectionRow{Symbol: rowSym, Quantity: q, AverageCost: a, RealizedPnL: r}
		if !pns.Valid {
			mtm = append(mtm, portfolio.MarkToMarket(row, decimal.Zero, false))
			continue
		}
		lp, _ := decimal.NewFromString(pns.String)
		mtm = append(mtm, portfolio.MarkToMarket(row, lp, true))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	lastPx := decimal.NewFromInt(13)
	wantMV := lastPx.Mul(qty)
	wantUnreal := lastPx.Sub(avg).Mul(qty)

	tot := portfolio.SumTotals(mtm)
	if !tot.TotalMarketValue.Equal(wantMV) {
		t.Fatalf("total_market_value got %s want %s", tot.TotalMarketValue, wantMV)
	}
	if !tot.TotalUnrealizedPnL.Equal(wantUnreal) {
		t.Fatalf("total_unrealized_pnl got %s want %s", tot.TotalUnrealizedPnL, wantUnreal)
	}
	if !tot.TotalRealizedPnL.Equal(real) {
		t.Fatalf("total_realized_pnl got %s want %s", tot.TotalRealizedPnL, real)
	}
}

// TestApplyPrice_ShuffledAppend_LatestInProjection verifies inserts out of event_time order;
// full replay applies in (event_time, event_id) order so prices_projection holds the last mark.
func TestApplyPrice_ShuffledAppend_LatestInProjection(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	parts := eventsPricePartitions()
	const sym = "ZZEVTPXORD"
	part, err := config.PricePartitionForSymbol(parts, sym)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPricePartition(t, ctx, pool, part, []string{sym})
	t.Cleanup(func() { cleanupPricePartition(t, ctx, pool, part, []string{sym}) })

	repo := NewPostgresStore(pool)
	t0 := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	ev1 := priceUpdatedEnvelope(part, "px-ord-a", t0, sym, decimal.NewFromInt(10))
	ev2 := priceUpdatedEnvelope(part, "px-ord-b", t0.Add(time.Millisecond), sym, decimal.NewFromInt(20))
	ev3 := priceUpdatedEnvelope(part, "px-ord-c", t0.Add(2*time.Millisecond), sym, decimal.NewFromInt(30))

	for _, ev := range []domain.EventEnvelope{ev3, ev1, ev2} {
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append %s: %v", ev.IdempotencyKey, err)
		}
	}

	pw := newPricePartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, part)
	if err := pw.rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	var pstr string
	var asOf time.Time
	if err := pool.QueryRow(ctx, `
		SELECT price::text, as_of FROM prices_projection WHERE symbol = $1
	`, sym).Scan(&pstr, &asOf); err != nil {
		t.Fatalf("prices_projection: %v", err)
	}
	got, err := decimal.NewFromString(pstr)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(decimal.NewFromInt(30)) {
		t.Fatalf("price got %s want 30", got)
	}
	if !asOf.Equal(ev3.EventTime.UTC()) {
		t.Fatalf("as_of got %v want %v", asOf, ev3.EventTime.UTC())
	}
}

func TestApplyPrice_Restart_NoDoubleApply(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	parts := eventsPricePartitions()
	const sym = "ZZEVTPXRST"
	part, err := config.PricePartitionForSymbol(parts, sym)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPricePartition(t, ctx, pool, part, []string{sym})
	t.Cleanup(func() { cleanupPricePartition(t, ctx, pool, part, []string{sym}) })

	repo := NewPostgresStore(pool)
	for i, idem := range []string{"px-r1", "px-r2", "px-r3"} {
		ev := priceUpdatedEnvelope(part, idem, time.Now().UTC().Add(time.Duration(i)*time.Millisecond), sym, decimal.NewFromInt(int64(100+i)))
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append %s: %v", idem, err)
		}
	}

	pw1 := newPricePartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, part)
	if err := pw1.rebuild(ctx); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	cur1 := loadCursor(t, ctx, pool, part)
	var p1 string
	if err := pool.QueryRow(ctx, `SELECT price::text FROM prices_projection WHERE symbol = $1`, sym).Scan(&p1); err != nil {
		t.Fatalf("price after apply: %v", err)
	}

	pw2 := newPricePartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, part)
	if err := pw2.rebuild(ctx); err != nil {
		t.Fatalf("restart rebuild: %v", err)
	}
	if err := pw2.pumpAfterCursor(ctx); err != nil {
		t.Fatalf("incremental after restart: %v", err)
	}

	cur2 := loadCursor(t, ctx, pool, part)
	if cur2.Time != cur1.Time || cur2.ID != cur1.ID {
		t.Fatalf("cursor changed after no-op incremental: %+v -> %+v", cur1, cur2)
	}
	var p2 string
	if err := pool.QueryRow(ctx, `SELECT price::text FROM prices_projection WHERE symbol = $1`, sym).Scan(&p2); err != nil {
		t.Fatalf("price read: %v", err)
	}
	if p2 != p1 {
		t.Fatalf("price changed after restart: %s -> %s", p1, p2)
	}

	tail, err := repo.FetchAfter(ctx, part, cur2, 50)
	if err != nil {
		t.Fatalf("FetchAfter: %v", err)
	}
	if len(tail) != 0 {
		t.Fatalf("expected no tail after full apply, got %d events", len(tail))
	}
}

func TestApplyPrice_SymbolReturns_DayBoundaryAndWindow(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	parts := eventsPricePartitions()
	const sym = "ZZEVTPXRET"
	part, err := config.PricePartitionForSymbol(parts, sym)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPricePartition(t, ctx, pool, part, []string{sym})
	t.Cleanup(func() { cleanupPricePartition(t, ctx, pool, part, []string{sym}) })

	repo := NewPostgresStore(pool)
	start := time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC)

	// Same UTC day twice: second close should replace first; still one symbol_returns row.
	evSameDayA := priceUpdatedEnvelope(part, "ret-day-a", start, sym, decimal.NewFromInt(100))
	evSameDayB := priceUpdatedEnvelope(part, "ret-day-b", start.Add(2*time.Hour), sym, decimal.NewFromInt(101))
	for _, ev := range []domain.EventEnvelope{evSameDayA, evSameDayB} {
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append same-day event: %v", err)
		}
	}
	pw := newPricePartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, part)
	if err := pw.rebuild(ctx); err != nil {
		t.Fatalf("rebuild same-day: %v", err)
	}

	var sameDayCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM symbol_returns WHERE symbol = $1`, sym).Scan(&sameDayCount); err != nil {
		t.Fatalf("count symbol_returns: %v", err)
	}
	if sameDayCount != 1 {
		t.Fatalf("symbol_returns rows after same-day updates: got %d want 1", sameDayCount)
	}

	// Add 61 more days (total 62 dates) -> window should keep latest 60.
	for i := 1; i <= 61; i++ {
		ev := priceUpdatedEnvelope(
			part,
			"ret-window-"+time.Duration(i).String(),
			start.AddDate(0, 0, i),
			sym,
			decimal.NewFromInt(101+int64(i)),
		)
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append day %d: %v", i, err)
		}
	}
	if err := pw.pumpAfterCursor(ctx); err != nil {
		t.Fatalf("pump after cursor: %v", err)
	}

	var kept int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM symbol_returns WHERE symbol = $1`, sym).Scan(&kept); err != nil {
		t.Fatalf("count kept symbol_returns: %v", err)
	}
	if kept != symbolReturnsWindowN {
		t.Fatalf("symbol_returns window rows: got %d want %d", kept, symbolReturnsWindowN)
	}

	// earliest kept date should be start + 2 days (dates 0..61 existed, keep 2..61).
	var oldest time.Time
	if err := pool.QueryRow(ctx, `SELECT MIN(return_date) FROM symbol_returns WHERE symbol = $1`, sym).Scan(&oldest); err != nil {
		t.Fatalf("min return_date: %v", err)
	}
	wantOldest := start.AddDate(0, 0, 2)
	if oldest.UTC().Year() != wantOldest.UTC().Year() ||
		oldest.UTC().Month() != wantOldest.UTC().Month() ||
		oldest.UTC().Day() != wantOldest.UTC().Day() {
		t.Fatalf("oldest kept date got %s want %s", oldest.Format("2006-01-02"), wantOldest.Format("2006-01-02"))
	}
}

func TestInsertPortfolioSnapshot_Idempotent(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)
	pid := uuid.New()
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	snap := json.RawMessage(`{"format":"rtpre_trade_positions_v1","positions":[]}`)
	asOf := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	eid := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got, err := repo.InsertPortfolioSnapshot(ctx, pid, asOf, eid, snap)
	if err != nil {
		t.Fatalf("InsertPortfolioSnapshot: %v", err)
	}
	if !got.Inserted {
		t.Fatalf("first insert: want Inserted true")
	}

	got2, err := repo.InsertPortfolioSnapshot(ctx, pid, asOf, eid, snap)
	if err != nil {
		t.Fatalf("InsertPortfolioSnapshot retry: %v", err)
	}
	if got2.Inserted {
		t.Fatalf("retry same checkpoint: want Inserted false")
	}

	later := asOf.Add(time.Hour)
	got3, err := repo.InsertPortfolioSnapshot(ctx, pid, later, eid, snap)
	if err != nil {
		t.Fatalf("InsertPortfolioSnapshot second checkpoint: %v", err)
	}
	if !got3.Inserted {
		t.Fatalf("second checkpoint: want Inserted true")
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM portfolio_snapshots WHERE portfolio_id = $1`, pid).Scan(&n); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if n != 2 {
		t.Fatalf("snapshot rows: got %d want 2", n)
	}

	var latestID int64
	if err := pool.QueryRow(ctx, `
		SELECT id FROM portfolio_snapshots
		WHERE portfolio_id = $1
		ORDER BY as_of_event_time DESC, as_of_event_id DESC
		LIMIT 1
	`, pid).Scan(&latestID); err != nil {
		t.Fatalf("latest snapshot: %v", err)
	}
	var laterRowID int64
	if err := pool.QueryRow(ctx, `
		SELECT id FROM portfolio_snapshots
		WHERE portfolio_id = $1 AND as_of_event_time = $2
	`, pid, later).Scan(&laterRowID); err != nil {
		t.Fatalf("later row id: %v", err)
	}
	if latestID != laterRowID {
		t.Fatalf("latest by apply key: got id %d want %d (later checkpoint)", latestID, laterRowID)
	}

	if _, err := repo.InsertPortfolioSnapshot(ctx, pid, time.Time{}, eid, snap); err == nil {
		t.Fatalf("zero as_of_event_time: want error")
	}
	if _, err := repo.InsertPortfolioSnapshot(ctx, pid, asOf, uuid.Nil, snap); err == nil {
		t.Fatalf("nil as_of_event_id: want error")
	}
}

func TestLoadLatestPortfolioSnapshot(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)
	pid := uuid.New()
	pidStr := pid.String()
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	a := portfolio.NewAggregate()
	_ = a.ApplyEvent(pidStr, tradeEnvelope(pid, "snap-1", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), "t1", 7))
	body, err := portfolio.MarshalTradePositionsSnapshotV1(a, pidStr)
	if err != nil {
		t.Fatalf("MarshalTradePositionsSnapshotV1: %v", err)
	}

	e1 := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	e2 := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if _, err := repo.InsertPortfolioSnapshot(ctx, pid, t1, e1, body); err != nil {
		t.Fatalf("InsertPortfolioSnapshot 1: %v", err)
	}
	if _, err := repo.InsertPortfolioSnapshot(ctx, pid, t2, e2, body); err != nil {
		t.Fatalf("InsertPortfolioSnapshot 2: %v", err)
	}

	got, err := repo.LoadLatestPortfolioSnapshot(ctx, pid)
	if err != nil {
		t.Fatalf("LoadLatestPortfolioSnapshot: %v", err)
	}
	if !got.Found {
		t.Fatal("Found")
	}
	if !got.AsOf.Time.Equal(t2.UTC()) || got.AsOf.ID != e2 {
		t.Fatalf("AsOf got (%v,%s) want (%v,%s)", got.AsOf.Time, got.AsOf.ID, t2.UTC(), e2)
	}
	if len(got.SnapshotJSON) == 0 {
		t.Fatal("SnapshotJSON")
	}
	lot := got.Aggregate.Lot(pidStr, "AAPL")
	if !lot.Quantity.Equal(decimal.NewFromInt(7)) {
		t.Fatalf("hydrated quantity got %s", lot.Quantity)
	}

	empty, err := repo.LoadLatestPortfolioSnapshot(ctx, uuid.New())
	if err != nil {
		t.Fatalf("LoadLatestPortfolioSnapshot empty: %v", err)
	}
	if empty.Found || empty.Aggregate != nil {
		t.Fatalf("empty portfolio: Found=%v agg=%v", empty.Found, empty.Aggregate)
	}
}

func TestRebuild_FromSnapshotWhenCursorZero(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)
	pid := uuid.New()
	pidStr := pid.String()
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	ev1 := tradeEnvelope(pid, "idem-snap-1", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC), "t1", 1)
	ev2 := tradeEnvelope(pid, "idem-snap-2", time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC), "t2", 2)
	if _, err := repo.Append(ctx, ev1); err != nil {
		t.Fatalf("append ev1: %v", err)
	}
	if _, err := repo.Append(ctx, ev2); err != nil {
		t.Fatalf("append ev2: %v", err)
	}

	a := portfolio.NewAggregate()
	if err := a.ApplyEvent(pidStr, ev1); err != nil {
		t.Fatalf("apply ev1 mem: %v", err)
	}
	body, err := portfolio.MarshalTradePositionsSnapshotV1(a, pidStr)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if _, err := repo.InsertPortfolioSnapshot(ctx, pid, ev1.EventTime, ev1.EventID, body); err != nil {
		t.Fatalf("InsertPortfolioSnapshot: %v", err)
	}

	pw := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw.rebuild(ctx); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	want := decimal.NewFromInt(3)
	got := pw.agg.Quantity(pidStr, "AAPL")
	if !got.Equal(want) {
		t.Fatalf("quantity got %s want %s", got, want)
	}
}

// TestRestartRecovery_ExitCriteria_SnapshotMidStream applies the same deterministic trade
// sequence as a no-restart control run; snapshots fire (every batch); a fresh partitionWorker
// rebuild + pump must leave positions_projection identical to control (watermark 0).
func TestRestartRecovery_ExitCriteria_SnapshotMidStream(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)

	tBase := time.Date(2026, 7, 21, 15, 0, 0, 0, time.UTC)
	seqFor := func(pid uuid.UUID) []domain.EventEnvelope {
		return []domain.EventEnvelope{
			tradeEnvelope(pid, "exit-crit-a", tBase, "xa", 2),
			tradeEnvelope(pid, "exit-crit-b", tBase.Add(time.Millisecond), "xb", 3),
			tradeEnvelope(pid, "exit-crit-c", tBase.Add(2*time.Millisecond), "xc", 5),
		}
	}

	pidGolden := uuid.New()
	cleanupPortfolio(t, ctx, pool, pidGolden)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pidGolden) })
	goldAgg := portfolio.NewAggregate()
	goldenStr := pidGolden.String()
	for _, ev := range seqFor(pidGolden) {
		if err := goldAgg.ApplyEvent(goldenStr, ev); err != nil {
			t.Fatalf("golden apply: %v", err)
		}
	}
	wantLot := goldAgg.Lot(goldenStr, "AAPL")

	pidCtrl := uuid.New()
	cleanupPortfolio(t, ctx, pool, pidCtrl)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pidCtrl) })
	for _, ev := range seqFor(pidCtrl) {
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("control append: %v", err)
		}
	}
	pwCtrl := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pidCtrl, 0, 0)
	if err := pwCtrl.rebuild(ctx); err != nil {
		t.Fatalf("control rebuild: %v", err)
	}
	assertPositionLotProjectionEqual(t, ctx, pool, pidCtrl, "AAPL", wantLot)

	pid := uuid.New()
	cleanupPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })
	seq := seqFor(pid)
	if _, err := repo.Append(ctx, seq[0]); err != nil {
		t.Fatalf("append ev1: %v", err)
	}
	pw1 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 1, 0)
	if err := pw1.rebuild(ctx); err != nil {
		t.Fatalf("rebuild after first trade: %v", err)
	}
	var snapN int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM portfolio_snapshots WHERE portfolio_id = $1`, pid).Scan(&snapN); err != nil {
		t.Fatal(err)
	}
	if snapN < 1 {
		t.Fatalf("expected snapshot after first trade, got count %d", snapN)
	}
	if _, err := repo.Append(ctx, seq[1]); err != nil {
		t.Fatalf("append ev2: %v", err)
	}
	if _, err := repo.Append(ctx, seq[2]); err != nil {
		t.Fatalf("append ev3: %v", err)
	}
	partitionPumpUntilIdle(t, ctx, pw1, pool, pid, 30)
	assertPositionLotProjectionEqual(t, ctx, pool, pid, "AAPL", wantLot)

	pw2 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 1, 0)
	if err := pw2.rebuild(ctx); err != nil {
		t.Fatalf("restart rebuild: %v", err)
	}
	partitionPumpUntilIdle(t, ctx, pw2, pool, pid, 30)
	assertPositionLotProjectionEqual(t, ctx, pool, pid, "AAPL", wantLot)

	if _, err := pool.Exec(ctx, `DELETE FROM portfolio_snapshots WHERE portfolio_id = $1`, pid); err != nil {
		t.Fatalf("delete snapshots: %v", err)
	}
	pw3 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw3.rebuild(ctx); err != nil {
		t.Fatalf("rebuild without snapshot rows: %v", err)
	}
	partitionPumpUntilIdle(t, ctx, pw3, pool, pid, 30)
	assertPositionLotProjectionEqual(t, ctx, pool, pid, "AAPL", wantLot)
}

// TestRestartRecovery_ExitCriteria_FullReplayNoSnapshotRows wipes positions_projection and
// projection_cursor (events retained) and rebuilds with no portfolio_snapshots rows — full
// replay path must match the same golden as a single uninterrupted apply.
func TestRestartRecovery_ExitCriteria_FullReplayNoSnapshotRows(t *testing.T) {
	ctx := context.Background()
	pool := newIntegrationPool(t)
	repo := NewPostgresStore(pool)

	tBase := time.Date(2026, 7, 22, 9, 0, 0, 0, time.UTC)
	pid := uuid.New()
	pidStr := pid.String()
	cleanupPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupPortfolio(t, ctx, pool, pid) })

	seq := []domain.EventEnvelope{
		tradeEnvelope(pid, "exit-fr-a", tBase, "fa", 4),
		tradeEnvelope(pid, "exit-fr-b", tBase.Add(time.Millisecond), "fb", 1),
	}
	goldAgg := portfolio.NewAggregate()
	for _, ev := range seq {
		if err := goldAgg.ApplyEvent(pidStr, ev); err != nil {
			t.Fatalf("golden apply: %v", err)
		}
	}
	wantLot := goldAgg.Lot(pidStr, "AAPL")

	for _, ev := range seq {
		if _, err := repo.Append(ctx, ev); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	pw1 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw1.rebuild(ctx); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	assertPositionLotProjectionEqual(t, ctx, pool, pid, "AAPL", wantLot)

	var nSnap int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM portfolio_snapshots WHERE portfolio_id = $1`, pid).Scan(&nSnap); err != nil {
		t.Fatal(err)
	}
	if nSnap != 0 {
		t.Fatalf("expected no snapshot rows with policy off, got %d", nSnap)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM positions_projection WHERE portfolio_id = $1`, pid); err != nil {
		t.Fatalf("delete positions: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM projection_cursor WHERE portfolio_id = $1`, pid); err != nil {
		t.Fatalf("delete cursor: %v", err)
	}

	pw2 := newPartitionWorker(repo, zap.NewNop(), time.Second, 0, 0, pid, 0, 0)
	if err := pw2.rebuild(ctx); err != nil {
		t.Fatalf("full replay rebuild: %v", err)
	}
	partitionPumpUntilIdle(t, ctx, pw2, pool, pid, 30)
	assertPositionLotProjectionEqual(t, ctx, pool, pid, "AAPL", wantLot)
}
