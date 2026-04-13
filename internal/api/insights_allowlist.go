package api

import "strings"

// SymbolAllowlistFromInsightsContext builds upper-case symbols from portfolio positions,
// unpriced symbols, and risk exposure (including concentration and volatility rows).
func SymbolAllowlistFromInsightsContext(ctx *InsightsExplainContext) map[string]struct{} {
	if ctx == nil {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{})
	add := func(sym string) {
		s := strings.ToUpper(strings.TrimSpace(sym))
		if s != "" {
			out[s] = struct{}{}
		}
	}
	for _, p := range ctx.Portfolio.Positions {
		add(p.Symbol)
	}
	for _, u := range ctx.Portfolio.UnpricedSymbols {
		add(u)
	}
	for _, e := range ctx.Risk.Exposure {
		add(e.Symbol)
	}
	for _, e := range ctx.Risk.Concentration.TopN {
		add(e.Symbol)
	}
	for _, row := range ctx.Risk.Volatility.BySymbol {
		add(row.Symbol)
	}
	return out
}
