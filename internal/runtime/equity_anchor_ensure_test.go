package runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
)

type fakeAnchorStore struct {
	mu           sync.Mutex
	anchors      map[string]decimal.Decimal
	upsertCalls  int
	insertCalls  int
}

func anchorStoreKey(portfolioID uuid.UUID, anchorDate time.Time) string {
	d := time.Date(anchorDate.Year(), anchorDate.Month(), anchorDate.Day(), 0, 0, 0, 0, time.UTC)
	return portfolioID.String() + "|" + d.Format("2006-01-02")
}

func (f *fakeAnchorStore) UpsertEquityAnchor(_ context.Context, portfolioID uuid.UUID, anchorDate time.Time, equity decimal.Decimal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertCalls++
	if f.anchors == nil {
		f.anchors = make(map[string]decimal.Decimal)
	}
	f.anchors[anchorStoreKey(portfolioID, anchorDate)] = equity
	return nil
}

func (f *fakeAnchorStore) LoadEquityAnchorForPortfolioDate(_ context.Context, portfolioID uuid.UUID, anchorDate time.Time) (decimal.Decimal, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	eq, ok := f.anchors[anchorStoreKey(portfolioID, anchorDate)]
	return eq, ok, nil
}

func (f *fakeAnchorStore) InsertEquityAnchorIfMissing(_ context.Context, portfolioID uuid.UUID, anchorDate time.Time, equity decimal.Decimal) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertCalls++
	if f.anchors == nil {
		f.anchors = make(map[string]decimal.Decimal)
	}
	k := anchorStoreKey(portfolioID, anchorDate)
	if _, exists := f.anchors[k]; exists {
		return false, nil
	}
	f.anchors[k] = equity
	return true, nil
}

func testSyncTarget(pid uuid.UUID) events.AlpacaSyncTarget {
	return events.AlpacaSyncTarget{
		PortfolioID:       pid,
		AlpacaAccountMode: "paper",
		AlpacaKeyID:       "k",
		AlpacaSecretKey:   "s",
		AlpacaBaseURL:     "https://paper-api.alpaca.markets",
	}
}

func TestEquityAnchorJob_EnsureDoesNotOverwriteExisting(t *testing.T) {
	pid := uuid.New()
	store := &fakeAnchorStore{
		anchors: map[string]decimal.Decimal{
			anchorStoreKey(pid, TodayAnchorDateUTC(time.Now())): decimal.RequireFromString("100000"),
		},
	}
	rest := &fakeREST{acct: alpaca.AccountSummary{Equity: decimal.RequireFromString("200000")}}
	job := &EquityAnchorJob{
		Anchor:  store,
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return rest, nil },
		Log:     zap.NewNop(),
	}
	job.EnsureTodayForTarget(context.Background(), testSyncTarget(pid))
	if store.insertCalls != 0 {
		t.Fatalf("InsertEquityAnchorIfMissing calls = %d, want 0", store.insertCalls)
	}
	eq, ok, _ := store.LoadEquityAnchorForPortfolioDate(context.Background(), pid, TodayAnchorDateUTC(time.Now()))
	if !ok || !eq.Equal(decimal.RequireFromString("100000")) {
		t.Fatalf("anchor equity = %s ok=%v, want 100000 unchanged", eq.String(), ok)
	}
}

func TestEquityAnchorJob_EnsureWritesWhenMissing(t *testing.T) {
	pid := uuid.New()
	store := &fakeAnchorStore{}
	rest := &fakeREST{acct: alpaca.AccountSummary{Equity: decimal.RequireFromString("100175.8")}}
	job := &EquityAnchorJob{
		Anchor:  store,
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return rest, nil },
		Log:     zap.NewNop(),
	}
	job.EnsureTodayForTarget(context.Background(), testSyncTarget(pid))
	if store.insertCalls != 1 {
		t.Fatalf("InsertEquityAnchorIfMissing calls = %d, want 1", store.insertCalls)
	}
	eq, ok, _ := store.LoadEquityAnchorForPortfolioDate(context.Background(), pid, TodayAnchorDateUTC(time.Now()))
	if !ok || !eq.Equal(decimal.RequireFromString("100175.8")) {
		t.Fatalf("anchor equity = %s ok=%v, want 100175.8", eq.String(), ok)
	}
}

func TestEquityAnchorJob_EnsureTodayAllMissing(t *testing.T) {
	pid := uuid.New()
	store := &fakeAnchorStore{}
	targets := &fakeTargetLister{out: []events.AlpacaSyncTarget{testSyncTarget(pid)}}
	rest := &fakeREST{acct: alpaca.AccountSummary{Equity: decimal.RequireFromString("99999")}}
	job := &EquityAnchorJob{
		Targets: targets,
		Anchor:  store,
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return rest, nil },
		Log:     zap.NewNop(),
	}
	job.EnsureTodayAllMissing(context.Background())
	if store.insertCalls != 1 {
		t.Fatalf("insert calls = %d, want 1", store.insertCalls)
	}
}
