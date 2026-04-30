package policy

import (
	"fmt"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/shopspring/decimal"
)

var (
	dec100   = decimal.NewFromInt(100)
	dec25000 = decimal.NewFromInt(25000)
)

// Evaluate runs deterministic rules in fixed order and aggregates violations.
// Kill-switch semantics: Env OR DB active counts as blocked (OR).
func Evaluate(intent Intent, snap Snapshot, cfg Config) Decision {
	out := Decision{
		PolicyConfigHash: PolicyConfigHash(cfg),
		InputsHash:       InputsHash(intent, snap),
	}

	sym := NormalizeSymbol(intent.Symbol)
	if sym == "" || !domain.IsValidSymbol(sym) {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleIntentSymbol,
			Detail: "symbol missing or invalid",
		})
	}
	if !domain.IsValidSide(intent.Side) {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleIntentSide,
			Detail: "side must be BUY or SELL",
		})
	}

	mark, hasMark := snap.MarkPriceBySymbol[sym]
	if !hasMark {
		// allow lookup by raw key if caller forgot to normalize
		for k, v := range snap.MarkPriceBySymbol {
			if NormalizeSymbol(k) == sym {
				mark = v
				hasMark = true
				break
			}
		}
	}

	curQty := decimal.Zero
	if snap.PositionQtyBySymbol != nil {
		if q, ok := snap.PositionQtyBySymbol[sym]; ok {
			curQty = q
		} else {
			for k, v := range snap.PositionQtyBySymbol {
				if NormalizeSymbol(k) == sym {
					curQty = v
					break
				}
			}
		}
	}

	tradeNotional, okN := tradeNotionalUSD(intent, mark, hasMark)
	tradeQty, okQ := tradeQtyShares(intent, mark, hasMark)

	// --- fixed order: kill_switch → data_validity → symbol lists → daily_loss → notional caps → position_pct → market_hours → PDT/blocks → rate_limit ---

	// 1. Kill switch (OR: env or DB row active)
	if snap.KillSwitchEnv || snap.KillSwitchDB {
		src := killSwitchSource(snap)
		out.Violations = append(out.Violations, Violation{
			Code:   RuleKillSwitch,
			Detail: fmt.Sprintf("trading halted (%s)", src),
		})
	}

	// 2. Data validity — fail-closed when a mark is required and missing or equity unusable
	if intent.Quantity != nil && !hasMark {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleDataMarkPrice,
			Detail: fmt.Sprintf("missing mark price for %s (required for quantity-sized order)", sym),
		})
	}
	if intent.NotionalUSD != nil && !hasMark && intent.Quantity == nil {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleDataMarkPrice,
			Detail: fmt.Sprintf("missing mark price for %s (required to derive shares from notional)", sym),
		})
	}
	if intent.Quantity != nil && intent.NotionalUSD != nil && hasMark {
		implied := intent.Quantity.Mul(mark)
		delta := implied.Sub(*intent.NotionalUSD).Abs()
		// tolerate rounding — more than 1 cent mismatch flags inconsistent intent
		if delta.GreaterThan(decimal.RequireFromString("0.01")) {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleDataMarkPrice,
				Detail: "quantity × mark does not match notional_usd",
			})
		}
	}
	if !snap.PortfolioEquity.IsPositive() {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleDataEquity,
			Detail: "portfolio equity must be positive for risk ratios",
		})
	}

	// 3. Symbol whitelist / blacklist
	if len(cfg.SymbolWhitelist) > 0 {
		if !stringInSortedSlice(sym, cfg.SymbolWhitelist) {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleSymbolWhitelist,
				Detail: fmt.Sprintf("%s not in whitelist", sym),
			})
		}
	}
	for _, b := range cfg.SymbolBlacklist {
		if NormalizeSymbol(b) == sym {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleSymbolBlacklist,
				Detail: fmt.Sprintf("%s is blacklisted", sym),
			})
			break
		}
	}

	// 4. Daily loss vs anchor
	if cfg.MaxDailyLossPct.IsPositive() && snap.EquityAnchor.IsPositive() {
		// lossPct = (equity - anchor) / anchor * 100; deny if lossPct <= -max (drawdown)
		ch := snap.PortfolioEquity.Sub(snap.EquityAnchor).Div(snap.EquityAnchor).Mul(dec100)
		if ch.LessThan(cfg.MaxDailyLossPct.Neg()) {
			out.Violations = append(out.Violations, Violation{
				Code: RuleDailyLossLimit,
				Detail: fmt.Sprintf("day drawdown %s%% exceeds limit %s%% (anchor=%s equity=%s)",
					ch.StringFixed(4), cfg.MaxDailyLossPct.Neg().StringFixed(4),
					snap.EquityAnchor.String(), snap.PortfolioEquity.String()),
			})
		}
	} else if cfg.MaxDailyLossPct.IsPositive() && snap.EquityAnchor.IsZero() {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleDailyLossAnchor,
			Detail: "equity anchor missing but max_daily_loss_pct is set (fail-closed)",
		})
	}

	// 5. Max per-order notional + daily notional budget
	if cfg.MaxOrderNotionalUSD.IsPositive() {
		if !okN {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleMaxOrderNotional,
				Detail: "cannot compute order notional (need qty×mark or notional_usd)",
			})
		} else if tradeNotional.GreaterThan(cfg.MaxOrderNotionalUSD) {
			out.Violations = append(out.Violations, Violation{
				Code: RuleMaxOrderNotional,
				Detail: fmt.Sprintf("order notional %s exceeds max %s",
					tradeNotional.String(), cfg.MaxOrderNotionalUSD.String()),
			})
		}
	}
	if cfg.MaxDailyNotionalUSD.IsPositive() && okN {
		if snap.DailyNotionalUsedUSD.Add(tradeNotional).GreaterThan(cfg.MaxDailyNotionalUSD) {
			out.Violations = append(out.Violations, Violation{
				Code: RuleMaxDailyNotional,
				Detail: fmt.Sprintf("daily notional used %s + order %s exceeds cap %s",
					snap.DailyNotionalUsedUSD.String(), tradeNotional.String(), cfg.MaxDailyNotionalUSD.String()),
			})
		}
	}

	// 6. Max position % (post-trade market value of symbol / equity)
	if cfg.MaxPositionPct.IsPositive() && snap.PortfolioEquity.IsPositive() {
		if !okQ || !hasMark {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleMaxPositionPct,
				Detail: "cannot evaluate position limit (need derivable share quantity and mark)",
			})
		} else {
			newQty := curQty
			if intent.Side == domain.SideBuy {
				newQty = newQty.Add(tradeQty)
			} else {
				newQty = newQty.Sub(tradeQty)
			}
			if newQty.IsNegative() {
				out.Violations = append(out.Violations, Violation{
					Code:   RuleNoShort,
					Detail: fmt.Sprintf("sell qty exceeds long position for %s (no shorting)", sym),
				})
			} else {
				posMV := newQty.Abs().Mul(mark)
				pct := posMV.Div(snap.PortfolioEquity).Mul(dec100)
				if pct.GreaterThan(cfg.MaxPositionPct) {
					out.Violations = append(out.Violations, Violation{
						Code: RuleMaxPositionPct,
						Detail: fmt.Sprintf("post-trade position %s%% exceeds max %s%%",
							pct.StringFixed(4), cfg.MaxPositionPct.String()),
					})
				}
			}
		}
	}

	// 7. Market hours (US equities regular session; weekends fail-closed)
	if !IsUSRegularSessionEquities(snap.NowNY) {
		out.Violations = append(out.Violations, Violation{
			Code:   RuleMarketHours,
			Detail: "outside US regular equity session (America/New_York, Mon–Fri 09:30–16:00)",
		})
	}

	// 8. PDT / blocks (Alpaca-shaped snapshot when present)
	if snap.OptionalBroker != nil {
		if snap.OptionalBroker.TradingBlocked {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleTradingBlocked,
				Detail: "broker reports trading_blocked",
			})
		}
		if snap.OptionalBroker.AccountBlocked {
			out.Violations = append(out.Violations, Violation{
				Code:   RuleAccountBlocked,
				Detail: "broker reports account_blocked",
			})
		}
		// Conservative stub: pattern day trader under $25k — block additional BUY exposure.
		if snap.OptionalBroker.PatternDayTrader &&
			intent.Side == domain.SideBuy &&
			snap.OptionalBroker.Equity.IsPositive() &&
			snap.OptionalBroker.Equity.LessThan(dec25000) {
			out.Violations = append(out.Violations, Violation{
				Code: RulePDTUnder25k,
				Detail: "pattern_day_trader with account equity under $25,000: BUY blocked (conservative stub; extend with day-trade counts in Phase 3+)",
			})
		}
	}

	// 9. Rate limit
	if cfg.MaxOrdersPerMinute > 0 && snap.OrdersLastMinute >= cfg.MaxOrdersPerMinute {
		out.Violations = append(out.Violations, Violation{
			Code: RuleRateLimit,
			Detail: fmt.Sprintf("orders in window %d >= max %d per minute",
				snap.OrdersLastMinute, cfg.MaxOrdersPerMinute),
		})
	}

	out.StrictOutcome = OutcomeAllow
	if len(out.Violations) > 0 {
		out.StrictOutcome = OutcomeDeny
	}
	out.EffectiveOutcome = out.StrictOutcome
	if cfg.Mode == ModeMonitor && out.StrictOutcome == OutcomeDeny {
		out.EffectiveOutcome = OutcomeAllow
	}

	primaryRule := "none"
	if len(out.Violations) > 0 {
		primaryRule = out.Violations[0].Code
	}
	killBlocked := false
	var ksSrc string
	for i := range out.Violations {
		if out.Violations[i].Code == RuleKillSwitch {
			killBlocked = true
			ksSrc = killSwitchSource(snap)
			break
		}
	}
	observability.ObservePolicyEvaluation(string(out.EffectiveOutcome), primaryRule, killBlocked, ksSrc)

	return out
}

func killSwitchSource(snap Snapshot) string {
	if snap.KillSwitchEnv && snap.KillSwitchDB {
		return "env+db"
	}
	if snap.KillSwitchEnv {
		return "env"
	}
	return "db"
}

func tradeNotionalUSD(intent Intent, mark decimal.Decimal, hasMark bool) (decimal.Decimal, bool) {
	if intent.NotionalUSD != nil {
		return *intent.NotionalUSD, true
	}
	if intent.Quantity != nil && hasMark && mark.IsPositive() {
		return intent.Quantity.Mul(mark), true
	}
	return decimal.Zero, false
}

func tradeQtyShares(intent Intent, mark decimal.Decimal, hasMark bool) (decimal.Decimal, bool) {
	if intent.Quantity != nil {
		return *intent.Quantity, true
	}
	if intent.NotionalUSD != nil && hasMark && mark.IsPositive() {
		return intent.NotionalUSD.Div(mark), true
	}
	return decimal.Zero, false
}

func stringInSortedSlice(sym string, list []string) bool {
	ns := NormalizeSymbol(sym)
	for _, x := range list {
		if NormalizeSymbol(x) == ns {
			return true
		}
	}
	return false
}

// ViolationCodes returns rule codes for convenience.
func ViolationCodes(vs []Violation) []string {
	out := make([]string, len(vs))
	for i := range vs {
		out[i] = vs[i].Code
	}
	return out
}

// CompactViolationSummary joins violation codes for tests/logging (stable order = evaluation order).
func CompactViolationSummary(vs []Violation) string {
	parts := make([]string, len(vs))
	for i := range vs {
		parts[i] = vs[i].Code
	}
	return strings.Join(parts, ",")
}
