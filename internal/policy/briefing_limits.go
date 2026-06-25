package policy

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

// BriefingLimits describes policy caps exposed to the briefing model.
type BriefingLimits struct {
	MaxOrderNotionalUSD    string `json:"max_order_notional_usd,omitempty"`
	MaxDailyNotionalUSD    string `json:"max_daily_notional_usd,omitempty"`
	MaxPositionPct         string `json:"max_position_pct,omitempty"`
	MaxDailyLossPct        string `json:"max_daily_loss_pct,omitempty"`
	MaxOrdersPerMinute     int    `json:"max_orders_per_minute,omitempty"`
	DailyNotionalUsedUSD   string `json:"daily_notional_used_usd,omitempty"`
	DailyNotionalRemaining string `json:"daily_notional_remaining_usd,omitempty"`
}

// BriefingLimitsJSON returns limits for the agent prompt (nil when all caps disabled).
func BriefingLimitsJSON(cfg Config, snap Snapshot) json.RawMessage {
	lim := BriefingLimits{}
	has := false
	if cfg.MaxOrderNotionalUSD.IsPositive() {
		lim.MaxOrderNotionalUSD = cfg.MaxOrderNotionalUSD.String()
		has = true
	}
	if cfg.MaxDailyNotionalUSD.IsPositive() {
		lim.MaxDailyNotionalUSD = cfg.MaxDailyNotionalUSD.String()
		has = true
		if snap.DailyNotionalUsedUSD.IsPositive() {
			lim.DailyNotionalUsedUSD = snap.DailyNotionalUsedUSD.String()
		}
		rem := cfg.MaxDailyNotionalUSD.Sub(snap.DailyNotionalUsedUSD)
		if rem.IsNegative() {
			rem = decimal.Zero
		}
		lim.DailyNotionalRemaining = rem.String()
	}
	if cfg.MaxPositionPct.IsPositive() {
		lim.MaxPositionPct = cfg.MaxPositionPct.String()
		has = true
	}
	if cfg.MaxDailyLossPct.IsPositive() {
		lim.MaxDailyLossPct = cfg.MaxDailyLossPct.String()
		has = true
	}
	if cfg.MaxOrdersPerMinute > 0 {
		lim.MaxOrdersPerMinute = cfg.MaxOrdersPerMinute
		has = true
	}
	if !has {
		return nil
	}
	b, err := json.Marshal(lim)
	if err != nil {
		return nil
	}
	return b
}
