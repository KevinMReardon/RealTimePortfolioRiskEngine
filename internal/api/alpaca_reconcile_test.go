package api

import (
	"testing"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
	"github.com/shopspring/decimal"
)

func TestReconcileQtyDrift_InSync(t *testing.T) {
	internal := []portfolio.ProjectionRow{
		{Symbol: "AAPL", Quantity: decimal.NewFromInt(10)},
	}
	broker := []alpaca.PositionRow{
		{Symbol: "aapl", Qty: decimal.NewFromInt(10)},
	}
	got := ReconcileQtyDrift(internal, broker)
	if len(got.Mismatches) != 0 {
		t.Fatalf("mismatches %+v", got.Mismatches)
	}
	if len(got.InternalOnlySymbols) != 0 || len(got.BrokerOnlySymbols) != 0 {
		t.Fatal("unexpected only lists")
	}
	if got.AggregateHash == "" {
		t.Fatal("empty hash")
	}
	h2 := ReconcileQtyDrift(internal, broker).AggregateHash
	if h2 != got.AggregateHash {
		t.Fatalf("hash not stable: %s vs %s", got.AggregateHash, h2)
	}
}

func TestReconcileQtyDrift_MismatchAndBrokerOnly(t *testing.T) {
	internal := []portfolio.ProjectionRow{
		{Symbol: "AAPL", Quantity: decimal.NewFromInt(5)},
	}
	broker := []alpaca.PositionRow{
		{Symbol: "AAPL", Qty: decimal.NewFromInt(3)},
		{Symbol: "MSFT", Qty: decimal.NewFromInt(2)},
	}
	got := ReconcileQtyDrift(internal, broker)
	if len(got.Mismatches) != 2 {
		t.Fatalf("want 2 mismatches got %d %+v", len(got.Mismatches), got.Mismatches)
	}
	if len(got.InternalOnlySymbols) != 0 {
		t.Fatalf("internal_only %v", got.InternalOnlySymbols)
	}
	if len(got.BrokerOnlySymbols) != 1 || got.BrokerOnlySymbols[0] != "MSFT" {
		t.Fatalf("broker_only %v", got.BrokerOnlySymbols)
	}
}

func TestPublicAccountOmitsBalances(t *testing.T) {
	a := alpaca.AccountSummary{
		ID:               "acc-x",
		Status:           "ACTIVE",
		Currency:         "USD",
		PatternDayTrader: true,
		TradingBlocked:   false,
		AccountBlocked:   false,
		CreatedAt:        time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		BuyingPower:      decimal.RequireFromString("999999"),
	}
	p := publicAccountFromSummary(a)
	if p == nil {
		t.Fatal("nil")
	}
	if p.ID != "acc-x" || p.Status != "ACTIVE" {
		t.Fatalf("%+v", p)
	}
}
