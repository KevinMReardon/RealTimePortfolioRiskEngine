package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca/bars"
)

// DailyBarsProvider returns up to `limit` daily OHLCV bars for symbol, oldest first.
// Implemented by the Alpaca bars client; nil providers cause the tool to return "not_configured".
type DailyBarsProvider interface {
	GetDailyBars(ctx context.Context, symbol string, limit int) ([]bars.Bar, error)
}

// WithBarsProvider injects the daily-bars provider used by research tools.
func (d *ToolDispatcher) WithBarsProvider(p DailyBarsProvider) *ToolDispatcher {
	d.barsProvider = p
	return d
}

// research tool names registered in tools.go.
const (
	ToolGetDailyBars            = "get_daily_bars"
	ToolGetTechnicalIndicators  = "get_technical_indicators"
	ToolGetMarketRegime         = "get_market_regime"

	// Reasonable per-call defaults.
	defaultBarsLimit            = 250
	maxBarsLimit                = 500
	defaultIndicatorLookback    = 200 // enough for SMA200
	defaultRegimeBenchmark      = "SPY"
	defaultRegimeLookbackBars   = 60
)

func researchToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name: ToolGetDailyBars,
			Description: "Return up to `limit` daily OHLCV bars (oldest first) for symbol from Alpaca Market Data. " +
				"Use when you need to inspect recent price action; combine with get_technical_indicators for derived signals.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string"},
					"limit":  map[string]any{"type": "integer", "minimum": 5, "maximum": maxBarsLimit},
				},
				"required": []string{"symbol"},
			},
		},
		{
			Name: ToolGetTechnicalIndicators,
			Description: "Return common technical signals (SMA20/50/200, RSI14, momentum10, annualized 20D volatility, " +
				"recent return %) computed from Alpaca daily bars. Use to evaluate trend and overbought/oversold conditions.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"symbol": map[string]any{"type": "string"},
				},
				"required": []string{"symbol"},
			},
		},
		{
			Name: ToolGetMarketRegime,
			Description: "Return a simple market-regime summary (bullish / bearish / chop) computed from the benchmark " +
				"(default SPY) over the trailing ~60 sessions. Use to bias trade ideas with the broad-market context.",
			InputSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"benchmark": map[string]any{"type": "string"},
				},
			},
		},
	}
}

type dailyBarsInput struct {
	Symbol string `json:"symbol"`
	Limit  int    `json:"limit,omitempty"`
}

func (d *ToolDispatcher) getDailyBars(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	if d.barsProvider == nil {
		return encodeToolOutput(map[string]any{"status": "not_configured", "error_code": "daily_bars_not_configured"})
	}
	var in dailyBarsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
	if sym == "" {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"symbol_required"}`), Error: "symbol required"}, fmt.Errorf("symbol required")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = defaultBarsLimit
	}
	if limit > maxBarsLimit {
		limit = maxBarsLimit
	}
	rows, err := d.barsProvider.GetDailyBars(ctx, sym, limit)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"daily_bars_failed"}`), Error: err.Error()}, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, b := range rows {
		out = append(out, map[string]any{
			"t": b.T.UTC().Format("2006-01-02"),
			"o": round4(b.O),
			"h": round4(b.H),
			"l": round4(b.L),
			"c": round4(b.C),
			"v": b.V,
		})
	}
	return encodeToolOutput(map[string]any{
		"status": "ok",
		"symbol": sym,
		"count":  len(out),
		"bars":   out,
	})
}

type indicatorsInput struct {
	Symbol string `json:"symbol"`
}

func (d *ToolDispatcher) getTechnicalIndicators(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	if d.barsProvider == nil {
		return encodeToolOutput(map[string]any{"status": "not_configured", "error_code": "indicators_not_configured"})
	}
	var in indicatorsInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
	}
	sym := strings.ToUpper(strings.TrimSpace(in.Symbol))
	if sym == "" {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"symbol_required"}`), Error: "symbol required"}, fmt.Errorf("symbol required")
	}
	rows, err := d.barsProvider.GetDailyBars(ctx, sym, defaultIndicatorLookback)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"indicators_failed"}`), Error: err.Error()}, err
	}
	closes := closesFromBars(rows)
	if len(closes) < 5 {
		return encodeToolOutput(map[string]any{
			"status":     "missing_data",
			"symbol":     sym,
			"bar_count":  len(closes),
			"reason":     "not enough history to compute indicators",
		})
	}
	last := closes[len(closes)-1]
	indicators := map[string]any{
		"last_close":       round4(last),
		"sma_20":           roundOrNil(sma(closes, 20)),
		"sma_50":           roundOrNil(sma(closes, 50)),
		"sma_200":          roundOrNil(sma(closes, 200)),
		"rsi_14":           roundOrNil(rsi(closes, 14)),
		"momentum_10":      roundOrNil(momentumPct(closes, 10)),
		"return_5d_pct":    roundOrNil(returnPct(closes, 5)),
		"return_20d_pct":   roundOrNil(returnPct(closes, 20)),
		"volatility_20d":  roundOrNil(annualizedVolatility(closes, 20)),
		"bar_count":        len(closes),
	}
	indicators["trend"] = classifyTrend(closes)
	return encodeToolOutput(map[string]any{
		"status":     "ok",
		"symbol":     sym,
		"indicators": indicators,
	})
}

type regimeInput struct {
	Benchmark string `json:"benchmark,omitempty"`
}

func (d *ToolDispatcher) getMarketRegime(ctx context.Context, raw json.RawMessage) (ToolCallResult, error) {
	if d.barsProvider == nil {
		return encodeToolOutput(map[string]any{"status": "not_configured", "error_code": "regime_not_configured"})
	}
	var in regimeInput
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &in); err != nil {
			return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"invalid_input"}`), Error: err.Error()}, err
		}
	}
	bench := strings.ToUpper(strings.TrimSpace(in.Benchmark))
	if bench == "" {
		bench = defaultRegimeBenchmark
	}
	rows, err := d.barsProvider.GetDailyBars(ctx, bench, defaultIndicatorLookback)
	if err != nil {
		return ToolCallResult{Output: json.RawMessage(`{"status":"error","error_code":"regime_failed"}`), Error: err.Error()}, err
	}
	closes := closesFromBars(rows)
	if len(closes) < 50 {
		return encodeToolOutput(map[string]any{
			"status":    "missing_data",
			"benchmark": bench,
			"reason":    "not enough benchmark history to classify regime",
		})
	}
	regime := classifyTrend(closes)
	out := map[string]any{
		"status":         "ok",
		"benchmark":      bench,
		"regime":         regime,
		"last_close":     round4(closes[len(closes)-1]),
		"sma_50":         roundOrNil(sma(closes, 50)),
		"sma_200":        roundOrNil(sma(closes, 200)),
		"return_20d_pct": roundOrNil(returnPct(closes, 20)),
		"return_60d_pct": roundOrNil(returnPct(closes, 60)),
		"volatility_20d": roundOrNil(annualizedVolatility(closes, 20)),
	}
	return encodeToolOutput(out)
}

// --- indicator math (server-side, deterministic) ---

func closesFromBars(rows []bars.Bar) []float64 {
	out := make([]float64, 0, len(rows))
	for _, b := range rows {
		if b.C > 0 {
			out = append(out, b.C)
		}
	}
	// Sort ascending by time should already be the case; defensive: leave caller order.
	return out
}

func sma(closes []float64, period int) *float64 {
	if period <= 0 || len(closes) < period {
		return nil
	}
	sum := 0.0
	for _, c := range closes[len(closes)-period:] {
		sum += c
	}
	v := sum / float64(period)
	return &v
}

// rsi implements Wilder's RSI on closes (default period 14).
func rsi(closes []float64, period int) *float64 {
	if period <= 0 || len(closes) <= period {
		return nil
	}
	// Initial averages from the first `period` changes.
	gains, losses := 0.0, 0.0
	for i := 1; i <= period; i++ {
		delta := closes[i] - closes[i-1]
		if delta > 0 {
			gains += delta
		} else {
			losses -= delta
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	// Wilder smoothing for the rest.
	for i := period + 1; i < len(closes); i++ {
		delta := closes[i] - closes[i-1]
		g, l := 0.0, 0.0
		if delta > 0 {
			g = delta
		} else {
			l = -delta
		}
		avgGain = (avgGain*float64(period-1) + g) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + l) / float64(period)
	}
	if avgLoss == 0 {
		v := 100.0
		return &v
	}
	rs := avgGain / avgLoss
	v := 100 - (100 / (1 + rs))
	return &v
}

// momentumPct is the % change of the last close vs the close `n` bars ago.
func momentumPct(closes []float64, n int) *float64 {
	if n <= 0 || len(closes) <= n {
		return nil
	}
	prev := closes[len(closes)-1-n]
	if prev == 0 {
		return nil
	}
	v := (closes[len(closes)-1]/prev - 1) * 100
	return &v
}

// returnPct is the same as momentumPct but explicit.
func returnPct(closes []float64, n int) *float64 {
	return momentumPct(closes, n)
}

// annualizedVolatility is the stdev of daily log returns over `period` bars * sqrt(252).
func annualizedVolatility(closes []float64, period int) *float64 {
	if period <= 1 || len(closes) <= period {
		return nil
	}
	tail := closes[len(closes)-period-1:]
	returns := make([]float64, 0, period)
	for i := 1; i < len(tail); i++ {
		if tail[i-1] <= 0 || tail[i] <= 0 {
			continue
		}
		returns = append(returns, math.Log(tail[i]/tail[i-1]))
	}
	if len(returns) < 2 {
		return nil
	}
	mean := 0.0
	for _, r := range returns {
		mean += r
	}
	mean /= float64(len(returns))
	sumSq := 0.0
	for _, r := range returns {
		sumSq += (r - mean) * (r - mean)
	}
	stdev := math.Sqrt(sumSq / float64(len(returns)-1))
	v := stdev * math.Sqrt(252) * 100
	return &v
}

// classifyTrend produces a bullish / bearish / chop label using SMA50/SMA200 + 20D return.
// Pure heuristic; the agent should still apply judgment.
func classifyTrend(closes []float64) string {
	if len(closes) < 50 {
		return "unknown"
	}
	last := closes[len(closes)-1]
	s50 := sma(closes, 50)
	s200 := sma(closes, 200)
	r20 := returnPct(closes, 20)
	bullishCount, bearishCount := 0, 0
	if s50 != nil && last > *s50 {
		bullishCount++
	} else if s50 != nil {
		bearishCount++
	}
	if s200 != nil && last > *s200 {
		bullishCount++
	} else if s200 != nil {
		bearishCount++
	}
	if r20 != nil && *r20 > 1 {
		bullishCount++
	} else if r20 != nil && *r20 < -1 {
		bearishCount++
	}
	switch {
	case bullishCount >= 2 && bearishCount == 0:
		return "bullish"
	case bearishCount >= 2 && bullishCount == 0:
		return "bearish"
	default:
		return "chop"
	}
}

func round4(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return math.Round(v*10000) / 10000
}

func roundOrNil(v *float64) any {
	if v == nil {
		return nil
	}
	return round4(*v)
}

// sortStable orders symbols for deterministic test output.
func sortStable(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
