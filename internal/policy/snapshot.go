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

// ApplyBrokerAccount overlays the live broker account onto snap. It sets OptionalBroker for the
// PDT/blocks rules and, when acct.Equity is positive, replaces PortfolioEquity with the broker's
// equity (cash + positions, sourced directly from Alpaca). This unblocks first-time buys when the
// internal projection has no positions yet but the account holds cash.
func ApplyBrokerAccount(snap *Snapshot, acct BrokerAccountSnapshot) {
	if snap == nil {
		return
	}
	acctCopy := acct
	snap.OptionalBroker = &acctCopy
	if acct.Equity.IsPositive() {
		snap.PortfolioEquity = acct.Equity
	}
}

// ApplyDailyUsage overlays measured "today's trading usage" onto snap so the daily-notional and
// rate-limit rules evaluate against real numbers rather than the zero defaults from BuildSnapshot.
// Callers compute these from proposed_trades (today's submitted notional and recent submit count).
func ApplyDailyUsage(snap *Snapshot, dailyNotionalUSD decimal.Decimal, ordersLastMinute int) {
	if snap == nil {
		return
	}
	if dailyNotionalUSD.IsPositive() {
		snap.DailyNotionalUsedUSD = dailyNotionalUSD
	}
	if ordersLastMinute > 0 {
		snap.OrdersLastMinute = ordersLastMinute
	}
}
