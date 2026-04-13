package ai

import (
	"encoding/json"
	"sort"
)

// DefaultUsedMetricsFromContextJSON returns a stable, server-derived path list for LLD §11
// auditability when the model omits used_metrics or returns non-JSON prose. Paths match the
// CONTEXT object (portfolio / risk / recent_events) and pass ValidateInsightOutput prefix rules.
func DefaultUsedMetricsFromContextJSON(contextJSON []byte) []string {
	if len(contextJSON) == 0 {
		return []string{}
	}
	var root struct {
		Portfolio struct {
			Totals struct {
				MarketValue   string `json:"market_value"`
				UnrealizedPnL string `json:"unrealized_pnl"`
				RealizedPnL   string `json:"realized_pnl"`
			} `json:"totals"`
		} `json:"portfolio"`
		Risk struct {
			Var95_1d string `json:"var_95_1d"`
			Vol      struct {
				Sigma1dPortfolio string            `json:"sigma_1d_portfolio"`
				BySymbol         []json.RawMessage `json:"by_symbol"`
			} `json:"volatility"`
			Exposure []json.RawMessage `json:"exposure"`
		} `json:"risk"`
		RecentEvents []json.RawMessage `json:"recent_events"`
	}
	if err := json.Unmarshal(contextJSON, &root); err != nil {
		return []string{}
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, 8)
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if root.Portfolio.Totals.MarketValue != "" {
		add("portfolio.totals.market_value")
	}
	if root.Portfolio.Totals.UnrealizedPnL != "" {
		add("portfolio.totals.unrealized_pnl")
	}
	if root.Portfolio.Totals.RealizedPnL != "" {
		add("portfolio.totals.realized_pnl")
	}
	if root.Risk.Var95_1d != "" {
		add("risk.var_95_1d")
	}
	if root.Risk.Vol.Sigma1dPortfolio != "" {
		add("risk.volatility.sigma_1d_portfolio")
	}
	if len(root.Risk.Exposure) > 0 {
		add("risk.exposure")
	}
	if len(root.Risk.Vol.BySymbol) > 0 {
		add("risk.volatility.by_symbol")
	}
	if len(root.RecentEvents) > 0 {
		add("recent_events")
	}
	sort.Strings(out)
	return out
}
