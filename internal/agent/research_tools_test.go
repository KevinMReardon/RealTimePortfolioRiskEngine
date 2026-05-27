package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca/bars"
)

type fakeBarsProvider struct {
	out   []bars.Bar
	err   error
	calls int
}

func (f *fakeBarsProvider) GetDailyBars(_ context.Context, _ string, _ int) ([]bars.Bar, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.out, nil
}

func syntheticTrendingUp(n int, start float64) []bars.Bar {
	out := make([]bars.Bar, 0, n)
	t := time.Now().Add(-time.Duration(n) * 24 * time.Hour)
	price := start
	for i := 0; i < n; i++ {
		price *= 1.005 // ~0.5% daily upward drift
		out = append(out, bars.Bar{T: t, O: price, H: price * 1.01, L: price * 0.99, C: price, V: 1_000_000})
		t = t.Add(24 * time.Hour)
	}
	return out
}

func TestResearchTools_NotConfiguredWithoutProvider(t *testing.T) {
	d := NewToolDispatcher(nil, nil, nil, nil)
	for _, tool := range []string{ToolGetDailyBars, ToolGetTechnicalIndicators, ToolGetMarketRegime} {
		in := json.RawMessage(`{"symbol":"AAPL"}`)
		if tool == ToolGetMarketRegime {
			in = json.RawMessage(`{}`)
		}
		res, err := d.Execute(context.Background(), ToolCallRequest{SessionID: "s", ToolName: tool, Input: in})
		if err != nil {
			t.Fatalf("%s err: %v", tool, err)
		}
		var payload map[string]any
		if jsonErr := json.Unmarshal(res.Output, &payload); jsonErr != nil {
			t.Fatalf("%s decode: %v", tool, jsonErr)
		}
		if payload["status"] != "not_configured" {
			t.Fatalf("%s status = %v, want not_configured", tool, payload["status"])
		}
	}
}

func TestResearchTools_DailyBarsHappyPath(t *testing.T) {
	fp := &fakeBarsProvider{out: syntheticTrendingUp(20, 100)}
	d := NewToolDispatcher(nil, nil, nil, nil).WithBarsProvider(fp)
	res, err := d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s",
		ToolName:  ToolGetDailyBars,
		Input:     json.RawMessage(`{"symbol":"AAPL","limit":10}`),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var payload struct {
		Status string
		Symbol string
		Count  int
		Bars   []map[string]any
	}
	if jsonErr := json.Unmarshal(res.Output, &payload); jsonErr != nil {
		t.Fatalf("decode: %v", jsonErr)
	}
	if payload.Status != "ok" {
		t.Fatalf("status = %s, want ok", payload.Status)
	}
	if payload.Symbol != "AAPL" {
		t.Fatalf("symbol = %s, want AAPL", payload.Symbol)
	}
	if payload.Count == 0 || len(payload.Bars) != payload.Count {
		t.Fatalf("count/bars mismatch: count=%d len=%d", payload.Count, len(payload.Bars))
	}
}

func TestResearchTools_DailyBarsProviderError(t *testing.T) {
	fp := &fakeBarsProvider{err: errors.New("upstream 500")}
	d := NewToolDispatcher(nil, nil, nil, nil).WithBarsProvider(fp)
	res, err := d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s",
		ToolName:  ToolGetDailyBars,
		Input:     json.RawMessage(`{"symbol":"AAPL"}`),
	})
	if err == nil {
		t.Fatal("expected upstream error to propagate")
	}
	var payload map[string]any
	_ = json.Unmarshal(res.Output, &payload)
	if payload["status"] != "error" {
		t.Fatalf("status = %v, want error", payload["status"])
	}
}

func TestResearchTools_TechnicalIndicatorsTrend(t *testing.T) {
	fp := &fakeBarsProvider{out: syntheticTrendingUp(220, 100)}
	d := NewToolDispatcher(nil, nil, nil, nil).WithBarsProvider(fp)
	res, err := d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s",
		ToolName:  ToolGetTechnicalIndicators,
		Input:     json.RawMessage(`{"symbol":"AAPL"}`),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var payload struct {
		Status     string
		Indicators map[string]any
	}
	if jsonErr := json.Unmarshal(res.Output, &payload); jsonErr != nil {
		t.Fatalf("decode: %v", jsonErr)
	}
	if payload.Status != "ok" {
		t.Fatalf("status = %s, want ok", payload.Status)
	}
	if payload.Indicators["trend"] != "bullish" {
		t.Fatalf("trend = %v, want bullish for synthetic uptrending series", payload.Indicators["trend"])
	}
	if payload.Indicators["sma_50"] == nil || payload.Indicators["sma_200"] == nil {
		t.Fatal("expected sma_50 and sma_200 to be populated for 220-bar history")
	}
	if payload.Indicators["rsi_14"] == nil {
		t.Fatal("expected rsi_14 to be populated")
	}
}

func TestResearchTools_TechnicalIndicatorsMissingData(t *testing.T) {
	fp := &fakeBarsProvider{out: syntheticTrendingUp(3, 100)} // too few bars
	d := NewToolDispatcher(nil, nil, nil, nil).WithBarsProvider(fp)
	res, err := d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s",
		ToolName:  ToolGetTechnicalIndicators,
		Input:     json.RawMessage(`{"symbol":"AAPL"}`),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var payload map[string]any
	_ = json.Unmarshal(res.Output, &payload)
	if payload["status"] != "missing_data" {
		t.Fatalf("status = %v, want missing_data", payload["status"])
	}
}

func TestResearchTools_MarketRegimeBullish(t *testing.T) {
	fp := &fakeBarsProvider{out: syntheticTrendingUp(220, 400)}
	d := NewToolDispatcher(nil, nil, nil, nil).WithBarsProvider(fp)
	res, err := d.Execute(context.Background(), ToolCallRequest{
		SessionID: "s",
		ToolName:  ToolGetMarketRegime,
		Input:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var payload struct {
		Status    string
		Benchmark string
		Regime    string
	}
	if jsonErr := json.Unmarshal(res.Output, &payload); jsonErr != nil {
		t.Fatalf("decode: %v", jsonErr)
	}
	if payload.Status != "ok" {
		t.Fatalf("status = %s, want ok", payload.Status)
	}
	if payload.Benchmark != "SPY" {
		t.Fatalf("benchmark = %s, want SPY (default)", payload.Benchmark)
	}
	if payload.Regime != "bullish" {
		t.Fatalf("regime = %s, want bullish", payload.Regime)
	}
}

func TestResearchTools_ToolDefinitionsIncludesResearch(t *testing.T) {
	defs := ToolDefinitions()
	want := map[string]bool{
		ToolGetDailyBars:           false,
		ToolGetTechnicalIndicators: false,
		ToolGetMarketRegime:        false,
	}
	for _, d := range defs {
		if _, ok := want[d.Name]; ok {
			want[d.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("ToolDefinitions missing %s", name)
		}
	}
}

func TestIndicatorMath_RSIBoundaries(t *testing.T) {
	closes := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	v := rsi(closes, 14)
	if v == nil {
		t.Fatal("rsi nil")
	}
	if *v < 99 {
		t.Fatalf("rsi for strictly rising series should be near 100; got %v", *v)
	}
}

func TestIndicatorMath_VolatilityFlatIsZero(t *testing.T) {
	closes := make([]float64, 60)
	for i := range closes {
		closes[i] = 100
	}
	v := annualizedVolatility(closes, 20)
	if v == nil {
		t.Fatal("vol nil")
	}
	if *v > 0.0001 {
		t.Fatalf("flat series volatility should be ~0, got %v", *v)
	}
}
