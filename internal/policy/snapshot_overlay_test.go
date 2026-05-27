package policy_test

import (
	"testing"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/shopspring/decimal"
)

func TestApplyBrokerAccount_overridesEquityAndSetsFlags(t *testing.T) {
	snap := policy.Snapshot{
		PortfolioEquity: decimal.RequireFromString("0"), // cash-only portfolio: positions sum to zero
	}
	policy.ApplyBrokerAccount(&snap, policy.BrokerAccountSnapshot{
		Equity:           decimal.RequireFromString("100000"),
		PatternDayTrader: true,
		TradingBlocked:   false,
		AccountBlocked:   false,
	})
	if snap.OptionalBroker == nil {
		t.Fatal("OptionalBroker should be set")
	}
	if !snap.OptionalBroker.PatternDayTrader {
		t.Fatal("PatternDayTrader should propagate")
	}
	want := decimal.RequireFromString("100000")
	if !snap.PortfolioEquity.Equal(want) {
		t.Fatalf("PortfolioEquity = %s, want %s (broker equity should overwrite cash-only zero)", snap.PortfolioEquity.String(), want.String())
	}
}

func TestApplyBrokerAccount_zeroEquityKeepsExistingPositionsValue(t *testing.T) {
	snap := policy.Snapshot{
		PortfolioEquity: decimal.RequireFromString("12345"),
	}
	policy.ApplyBrokerAccount(&snap, policy.BrokerAccountSnapshot{
		Equity: decimal.Zero,
	})
	if !snap.PortfolioEquity.Equal(decimal.RequireFromString("12345")) {
		t.Fatalf("PortfolioEquity = %s, want unchanged 12345 when broker equity is zero", snap.PortfolioEquity.String())
	}
	if snap.OptionalBroker == nil {
		t.Fatal("OptionalBroker should still be set even when equity is zero")
	}
}

func TestApplyDailyUsage_setsBothFields(t *testing.T) {
	snap := policy.Snapshot{}
	policy.ApplyDailyUsage(&snap, decimal.RequireFromString("500"), 3)
	if !snap.DailyNotionalUsedUSD.Equal(decimal.RequireFromString("500")) {
		t.Fatalf("DailyNotionalUsedUSD = %s, want 500", snap.DailyNotionalUsedUSD.String())
	}
	if snap.OrdersLastMinute != 3 {
		t.Fatalf("OrdersLastMinute = %d, want 3", snap.OrdersLastMinute)
	}
}

func TestApplyDailyUsage_doesNotClobberWithZero(t *testing.T) {
	snap := policy.Snapshot{
		DailyNotionalUsedUSD: decimal.RequireFromString("250"),
		OrdersLastMinute:     2,
	}
	policy.ApplyDailyUsage(&snap, decimal.Zero, 0)
	if !snap.DailyNotionalUsedUSD.Equal(decimal.RequireFromString("250")) {
		t.Fatalf("DailyNotionalUsedUSD should be preserved when zero is passed; got %s", snap.DailyNotionalUsedUSD.String())
	}
	if snap.OrdersLastMinute != 2 {
		t.Fatalf("OrdersLastMinute should be preserved when zero is passed; got %d", snap.OrdersLastMinute)
	}
}

func TestApplyBrokerAccount_nilSnapshotSafe(t *testing.T) {
	policy.ApplyBrokerAccount(nil, policy.BrokerAccountSnapshot{Equity: decimal.RequireFromString("1")})
}

func TestApplyDailyUsage_nilSnapshotSafe(t *testing.T) {
	policy.ApplyDailyUsage(nil, decimal.RequireFromString("1"), 1)
}
