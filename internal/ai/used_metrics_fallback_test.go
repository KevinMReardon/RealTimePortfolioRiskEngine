package ai

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestDefaultUsedMetricsFromContextJSON_stablePaths(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
  "portfolio": {"totals": {"market_value": "100", "unrealized_pnl": "1", "realized_pnl": "0"}},
  "risk": {
    "var_95_1d": "5",
    "volatility": {"sigma_1d_portfolio": "0.02", "by_symbol": [{"symbol": "AAPL"}]},
    "exposure": [{"symbol": "AAPL"}]
  },
  "recent_events": [{"event_type": "TradeExecuted"}]
}`)
	got := DefaultUsedMetricsFromContextJSON(raw)
	want := []string{
		"portfolio.totals.market_value",
		"portfolio.totals.realized_pnl",
		"portfolio.totals.unrealized_pnl",
		"recent_events",
		"risk.exposure",
		"risk.var_95_1d",
		"risk.volatility.by_symbol",
		"risk.volatility.sigma_1d_portfolio",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestDefaultUsedMetricsFromContextJSON_invalidJSON(t *testing.T) {
	t.Parallel()
	got := DefaultUsedMetricsFromContextJSON([]byte(`not-json`))
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v", got)
	}
}

func TestDefaultUsedMetricsFromContextJSON_roundTripJSON(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"portfolio":{"totals":{"market_value":"1"}},"risk":{"var_95_1d":"2"},"recent_events":[]}`)
	paths := DefaultUsedMetricsFromContextJSON(raw)
	for _, p := range paths {
		if err := ValidateInsightOutput(map[string]struct{}{}, `{"narrative":"Summary.","used_metrics":[]}`, []string{p}); err != nil {
			t.Fatalf("path %q failed validation: %v", p, err)
		}
	}
	payload, _ := json.Marshal(map[string]any{"narrative": "ok", "used_metrics": paths})
	if err := ValidateInsightOutput(map[string]struct{}{}, string(payload), paths); err != nil {
		t.Fatal(err)
	}
}
