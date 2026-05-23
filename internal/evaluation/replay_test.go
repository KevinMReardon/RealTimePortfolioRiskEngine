package evaluation

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/shopspring/decimal"
)

func TestLoadReplayFixture_and_metrics(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "sample_replay.json")
	f, err := LoadReplayFixture(path)
	if err != nil {
		t.Fatalf("LoadReplayFixture: %v", err)
	}
	if CountPolicyViolations(f.Proposals) != 1 {
		t.Fatalf("violations=%d", CountPolicyViolations(f.Proposals))
	}
	rets := make([]decimal.Decimal, 0, len(f.PeriodReturns))
	for _, s := range f.PeriodReturns {
		d, err := decimal.NewFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		rets = append(rets, d)
	}
	if CumulativePnL(rets).IsZero() {
		t.Fatal("expected non-zero cumulative pnl")
	}
	mock := NewMockREST()
	if _, err := mock.PlaceOrder(context.Background(), alpaca.PlaceOrderInput{Symbol: "AAPL", Side: "buy"}); err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if len(mock.Orders) != 1 {
		t.Fatalf("orders=%d", len(mock.Orders))
	}
}
