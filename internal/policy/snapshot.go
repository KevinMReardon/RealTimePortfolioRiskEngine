package policy

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// BuildSnapshot builds a policy evaluation snapshot from assembler input.
func BuildSnapshot(in portfolio.PortfolioAssemblerInput, equityAnchor decimal.Decimal, nowNY time.Time, killEnv, killDB bool) Snapshot {
	posQty := make(map[string]decimal.Decimal)
	marks := make(map[string]decimal.Decimal)
	totalMV := decimal.Zero

	for _, p := range in.Positions {
		sym := strings.TrimSpace(p.Symbol)
		if sym == "" {
			continue
		}
		posQty[sym] = p.Quantity
		if pm, ok := in.PriceBySymbol[sym]; ok && !pm.Price.IsZero() {
			marks[sym] = pm.Price
			if !p.Quantity.IsZero() {
				totalMV = totalMV.Add(p.Quantity.Abs().Mul(pm.Price))
			}
		}
	}

	return Snapshot{
		PortfolioEquity:      totalMV,
		PositionQtyBySymbol:  posQty,
		MarkPriceBySymbol:    marks,
		NowNY:                nowNY,
		EquityAnchor:         equityAnchor,
		KillSwitchEnv:        killEnv,
		KillSwitchDB:         killDB,
		DailyNotionalUsedUSD: decimal.Zero,
		OrdersLastMinute:     0,
	}
}
