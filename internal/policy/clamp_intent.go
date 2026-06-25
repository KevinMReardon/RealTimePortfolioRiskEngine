package policy

import (
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
)

// ClampIntentNotional reduces notional_usd to policy caps before evaluate/materialize.
// Quantity-only intents are unchanged (position % may still deny at evaluate).
func ClampIntentNotional(intent Intent, snap Snapshot, cfg Config) Intent {
	if intent.NotionalUSD == nil || !intent.NotionalUSD.IsPositive() {
		return intent
	}
	n := *intent.NotionalUSD
	if cfg.MaxOrderNotionalUSD.IsPositive() && n.GreaterThan(cfg.MaxOrderNotionalUSD) {
		n = cfg.MaxOrderNotionalUSD
	}
	if cfg.MaxDailyNotionalUSD.IsPositive() {
		rem := cfg.MaxDailyNotionalUSD.Sub(snap.DailyNotionalUsedUSD)
		if rem.IsNegative() {
			rem = decimal.Zero
		}
		if n.GreaterThan(rem) {
			n = rem
		}
	}
	if n.LessThan(alpaca.MinNotionalStockOrderUSD) {
		n = alpaca.MinNotionalStockOrderUSD
	}
	out := intent
	out.NotionalUSD = &n
	return out
}
