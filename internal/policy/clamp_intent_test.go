package policy_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
)

func TestClampIntentNotional_orderCap(t *testing.T) {
	t.Parallel()
	n := decimal.RequireFromString("10000")
	intent := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		NotionalUSD: &n,
	}
	cfg := policy.Config{MaxOrderNotionalUSD: decimal.RequireFromString("2000")}
	out := policy.ClampIntentNotional(intent, policy.Snapshot{}, cfg)
	if out.NotionalUSD == nil || !out.NotionalUSD.Equal(decimal.RequireFromString("2000")) {
		t.Fatalf("notional=%v want 2000", out.NotionalUSD)
	}
}
