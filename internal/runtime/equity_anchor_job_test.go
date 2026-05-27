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

type fakeTargetLister struct {
	out []events.AlpacaSyncTarget
	err error
}

func (f *fakeTargetLister) ListAlpacaSyncTargets(_ context.Context) ([]events.AlpacaSyncTarget, error) {
	return f.out, f.err
}

type fakeAnchorWriter struct {
	mu      sync.Mutex
	calls   int
	lastPID uuid.UUID
	lastEq  decimal.Decimal
	err     error
}

func (f *fakeAnchorWriter) UpsertEquityAnchor(_ context.Context, portfolioID uuid.UUID, _ time.Time, equity decimal.Decimal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastPID = portfolioID
	f.lastEq = equity
	return f.err
}

type fakeREST struct {
	acct alpaca.AccountSummary
	err  error
}

func (f *fakeREST) GetAccount(_ context.Context) (alpaca.AccountSummary, error) {
	return f.acct, f.err
}
func (f *fakeREST) ListPositions(_ context.Context) ([]alpaca.PositionRow, error) {
	return nil, nil
}
func (f *fakeREST) ListActivities(_ context.Context, _ alpaca.ListActivitiesRequest) (alpaca.ActivitiesPage, error) {
	return alpaca.ActivitiesPage{}, nil
}
func (f *fakeREST) ListOrders(_ context.Context, _ alpaca.ListOrdersRequest) ([]alpaca.OrderSnapshot, error) {
	return nil, nil
}
func (f *fakeREST) PlaceOrder(_ context.Context, _ alpaca.PlaceOrderInput) (alpaca.OrderSnapshot, error) {
	return alpaca.OrderSnapshot{}, nil
}

func TestEquityAnchorJob_TickHappyPath(t *testing.T) {
	pid := uuid.New()
	targets := &fakeTargetLister{
		out: []events.AlpacaSyncTarget{{
			PortfolioID:       pid,
			AlpacaAccountMode: "paper",
			AlpacaKeyID:       "k",
			AlpacaSecretKey:   "s",
			AlpacaBaseURL:     "https://paper-api.alpaca.markets",
		}},
	}
	writer := &fakeAnchorWriter{}
	rest := &fakeREST{acct: alpaca.AccountSummary{Equity: decimal.RequireFromString("100000")}}
	job := &EquityAnchorJob{
		Targets: targets,
		Anchor:  writer,
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return rest, nil },
		Log:     zap.NewNop(),
	}
	job.Tick(context.Background())
	if writer.calls != 1 {
		t.Fatalf("UpsertEquityAnchor calls = %d, want 1", writer.calls)
	}
	if writer.lastPID != pid {
		t.Fatalf("portfolio_id = %s, want %s", writer.lastPID, pid)
	}
	if !writer.lastEq.Equal(decimal.RequireFromString("100000")) {
		t.Fatalf("equity = %s, want 100000", writer.lastEq.String())
	}
}

func TestEquityAnchorJob_TickSkipsZeroEquity(t *testing.T) {
	pid := uuid.New()
	targets := &fakeTargetLister{
		out: []events.AlpacaSyncTarget{{
			PortfolioID:       pid,
			AlpacaAccountMode: "paper",
			AlpacaKeyID:       "k",
			AlpacaSecretKey:   "s",
		}},
	}
	writer := &fakeAnchorWriter{}
	rest := &fakeREST{acct: alpaca.AccountSummary{Equity: decimal.Zero}}
	job := &EquityAnchorJob{
		Targets: targets,
		Anchor:  writer,
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) { return rest, nil },
		Log:     zap.NewNop(),
	}
	job.Tick(context.Background())
	if writer.calls != 0 {
		t.Fatalf("UpsertEquityAnchor should not be called when broker equity is zero; calls=%d", writer.calls)
	}
}

func TestEquityAnchorJob_TickSkipsMissingKeys(t *testing.T) {
	pid := uuid.New()
	targets := &fakeTargetLister{
		out: []events.AlpacaSyncTarget{{
			PortfolioID: pid,
			// no keys
		}},
	}
	writer := &fakeAnchorWriter{}
	job := &EquityAnchorJob{
		Targets: targets,
		Anchor:  writer,
		NewREST: func(_ alpaca.RESTConfig) (alpaca.REST, error) {
			t.Fatal("NewREST should not be called when keys are empty")
			return nil, nil
		},
		Log: zap.NewNop(),
	}
	job.Tick(context.Background())
	if writer.calls != 0 {
		t.Fatalf("UpsertEquityAnchor should not be called when keys missing; calls=%d", writer.calls)
	}
}

func TestEquityAnchorJob_TickNoConfigSafe(t *testing.T) {
	j := &EquityAnchorJob{}
	j.Tick(context.Background())
}
