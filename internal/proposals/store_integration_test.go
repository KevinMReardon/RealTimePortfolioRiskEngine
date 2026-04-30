package proposals

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

func integrationDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("INTEGRATION_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set INTEGRATION_DATABASE_URL or TEST_DATABASE_URL to run integration tests (migrations including 000019 must be applied)")
	}
	return dsn
}

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, integrationDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return pool
}

func TestStore_proposal_lifecycle_and_anchors_integration(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	repo := events.NewPostgresStore(pool)
	store := NewStore(pool)

	portfolioID := uuid.New()
	userID := uuid.New()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM proposed_trades WHERE portfolio_id = $1`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM portfolio_equity_anchor WHERE portfolio_id = $1`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM portfolios WHERE portfolio_id = $1`, portfolioID)
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE user_id = $1`, userID)
	})

	if _, err := repo.CreatePortfolio(ctx, portfolioID, "proposal-int-test", "USD"); err != nil {
		t.Fatalf("CreatePortfolio: %v", err)
	}
	if _, err := repo.CreateUser(ctx, events.UserAccount{
		UserID:       userID,
		DisplayName:  "p-test",
		WorkEmail:    "proposal-int-test@example.com",
		PasswordHash: "x",
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	qty := decimal.RequireFromString("10")
	intent := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    &qty,
		OrderType:   "market",
		TimeInForce: "day",
	}
	snap := policy.Snapshot{
		PortfolioEquity:      decimal.RequireFromString("100000"),
		MarkPriceBySymbol:    map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("150")},
		PositionQtyBySymbol:  map[string]decimal.Decimal{},
		NowNY:                mustWednesdayNY(t),
		EquityAnchor:         decimal.RequireFromString("100000"),
		DailyNotionalUsedUSD: decimal.Zero,
	}
	cfg := policy.Config{
		Mode:                policy.ModeEnforce,
		MaxOrderNotionalUSD: decimal.RequireFromString("1000000"),
		MaxPositionPct:      decimal.RequireFromString("50"),
	}
	decision := policy.Evaluate(intent, snap, cfg)
	payloadHash := policy.OrderPayloadHash(intent)

	p, err := store.InsertProposal(ctx, InsertParams{
		PortfolioID: portfolioID,
		Intent:      intent,
		Decision:    decision,
		Mode:        cfg.Mode,
	})
	if err != nil {
		t.Fatalf("InsertProposal: %v", err)
	}
	if p.PayloadHash != payloadHash {
		t.Fatalf("payload_hash mismatch: stored %q want %q", p.PayloadHash, payloadHash)
	}
	if p.Status != "proposed" {
		t.Fatalf("status: %s", p.Status)
	}
	if p.RowVersion != 1 {
		t.Fatalf("row_version: %d", p.RowVersion)
	}

	list, err := store.ListByPortfolio(ctx, portfolioID, ListFilter{Status: strPtr("proposed")})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	err = store.ApproveProposal(ctx, ApproveParams{
		PortfolioID: portfolioID,
		ProposalID:  p.ProposalID,
		UserID:      userID,
		PayloadHash: "deadbeef",
		RowVersion:  1,
	})
	if err != ErrApproveConflict {
		t.Fatalf("wrong hash: want ErrApproveConflict, got %v", err)
	}

	if err := store.ApproveProposal(ctx, ApproveParams{
		PortfolioID: portfolioID,
		ProposalID:  p.ProposalID,
		UserID:      userID,
		PayloadHash: payloadHash,
		RowVersion:  1,
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	after, err := store.GetByIDForPortfolio(ctx, portfolioID, p.ProposalID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.Status != "approved" {
		t.Fatalf("after approve status = %q", after.Status)
	}
	if after.RowVersion != 2 {
		t.Fatalf("row_version should bump: %d", after.RowVersion)
	}

	day := time.Date(2020, 2, 3, 0, 0, 0, 0, time.UTC)
	if err := store.UpsertEquityAnchor(ctx, portfolioID, day, decimal.RequireFromString("100000")); err != nil {
		t.Fatalf("UpsertEquityAnchor: %v", err)
	}
	if err := store.UpsertEquityAnchor(ctx, portfolioID, day, decimal.RequireFromString("100001")); err != nil {
		t.Fatalf("UpsertEquityAnchor second: %v", err)
	}
}

func TestStore_kill_switch_integration(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	store := NewStore(pool)
	const reason = "proposals_integration_test_kill_switch"
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM kill_switch_events WHERE reason = $1`, reason)
	})

	_, ok, err := store.KillSwitchLatestActive(ctx)
	if err != nil {
		t.Fatalf("KillSwitchLatestActive: %v", err)
	}
	if ok {
		// another test may have left rows; we only assert after our append
		t.Log("kill_switch_events non-empty before test; continuing")
	}

	if err := store.AppendKillSwitchEvent(ctx, true, reason, nil); err != nil {
		t.Fatalf("AppendKillSwitchEvent: %v", err)
	}
	active, ok, err := store.KillSwitchLatestActive(ctx)
	if err != nil {
		t.Fatalf("KillSwitchLatestActive: %v", err)
	}
	if !ok || !active {
		t.Fatalf("expected latest active=true, ok=%v active=%v", ok, active)
	}
	if err := store.AppendKillSwitchEvent(ctx, false, reason, nil); err != nil {
		t.Fatalf("AppendKillSwitchEvent off: %v", err)
	}
	active2, _, err := store.KillSwitchLatestActive(ctx)
	if err != nil {
		t.Fatalf("KillSwitchLatestActive: %v", err)
	}
	if active2 {
		t.Fatal("expected latest active=false")
	}
	env, db := KillSwitchInputs(true, false, false)
	if !env || db {
		t.Fatalf("KillSwitchInputs OR: env=%v db=%v", env, db)
	}
	env, db = KillSwitchInputs(false, true, true)
	if env || !db {
		t.Fatalf("KillSwitchInputs db: env=%v db=%v", env, db)
	}
}

func strPtr(s string) *string { return &s }

func mustWednesdayNY(t *testing.T) time.Time {
	t.Helper()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return time.Date(2020, 1, 8, 14, 0, 0, 0, ny)
}
