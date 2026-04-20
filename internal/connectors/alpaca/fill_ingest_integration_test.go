package alpaca

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
)

func fillIntegrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set INTEGRATION_DATABASE_URL or TEST_DATABASE_URL to run fill integration tests (migrations applied)")
	}
	return dsn
}

func newFillIntegrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, fillIntegrationDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func cleanupFillPortfolio(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID) {
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

func fillIntegrationPricePartitions() []uuid.UUID {
	return config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 4)
}

func startFillTradeWorker(t *testing.T, pool *pgxpool.Pool, log *zap.Logger) context.CancelFunc {
	t.Helper()
	repo := events.NewPostgresStore(pool)
	w := events.NewWorker(repo, log, 30*time.Millisecond, 0, 0, 1, fillIntegrationPricePartitions())
	runCtx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = w.Run(runCtx)
	}()
	return cancel
}

func waitFillPositionQty(t *testing.T, ctx context.Context, pool *pgxpool.Pool, pid uuid.UUID, symbol string, want decimal.Decimal, timeout time.Duration) {
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

// TestIntegration_AlpacaFillDuplicateDoesNotDoublePosition wires real DB + ingestion + worker:
// same Alpaca activity id twice → one event row; position quantity is applied once only.
func TestIntegration_AlpacaFillDuplicateDoesNotDoublePosition(t *testing.T) {
	ctx := context.Background()
	pool := newFillIntegrationPool(t)
	pid := uuid.New()
	cleanupFillPortfolio(t, ctx, pool, pid)
	t.Cleanup(func() { cleanupFillPortfolio(t, ctx, pool, pid) })

	repo := events.NewPostgresStore(pool)
	svc := ingestion.NewService(repo)
	log := zap.NewNop()
	cancel := startFillTradeWorker(t, pool, log)
	defer cancel()

	act := ActivityRow{
		ID:              "alpaca-int-fill-1",
		ActivityType:    "FILL",
		TransactionTime: time.Now().UTC(),
		Symbol:          "aapl",
		Qty:             decimal.NewFromInt(7),
		Price:           decimal.NewFromInt(50),
		Side:            "buy",
	}

	o1, err := TryIngestFillActivity(ctx, svc, pid, act)
	if err != nil || o1 != OutcomeAppended {
		t.Fatalf("first ingest: out=%v err=%v", o1, err)
	}
	o2, err := TryIngestFillActivity(ctx, svc, pid, act)
	if err != nil || o2 != OutcomeDuplicate {
		t.Fatalf("second ingest: out=%v err=%v", o2, err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE portfolio_id = $1`, pid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("event rows: got %d want 1", n)
	}

	waitFillPositionQty(t, ctx, pool, pid, "AAPL", decimal.NewFromInt(7), 15*time.Second)

	o3, err := TryIngestFillActivity(ctx, svc, pid, act)
	if err != nil || o3 != OutcomeDuplicate {
		t.Fatalf("third ingest: out=%v err=%v", o3, err)
	}
	waitFillPositionQty(t, ctx, pool, pid, "AAPL", decimal.NewFromInt(7), 2*time.Second)
}
