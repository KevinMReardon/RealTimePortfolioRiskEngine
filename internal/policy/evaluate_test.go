package policy_test

import (
	"testing"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
	"github.com/shopspring/decimal"
)

func mustNY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// wednesdaySession is a weekday inside regular US equity hours (NY).
func wednesdaySession(t *testing.T) time.Time {
	t.Helper()
	ny := mustNY(t)
	return time.Date(2020, 1, 8, 14, 30, 0, 0, ny) // Wed
}

func saturday(t *testing.T) time.Time {
	t.Helper()
	ny := mustNY(t)
	return time.Date(2020, 1, 11, 12, 0, 0, 0, ny)
}

func baseCfg() policy.Config {
	return policy.Config{
		Mode:                policy.ModeEnforce,
		MaxOrderNotionalUSD: decimal.RequireFromString("100000"),
		MaxDailyNotionalUSD: decimal.RequireFromString("500000"),
		MaxPositionPct:      decimal.RequireFromString("100"),
		MaxDailyLossPct:     decimal.Zero,
		MaxOrdersPerMinute:  100,
	}
}

func qty(q string) *decimal.Decimal {
	d := decimal.RequireFromString(q)
	return &d
}

func TestEvaluate_killSwitch_OR(t *testing.T) {
	c := baseCfg()
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		PositionQtyBySymbol: map[string]decimal.Decimal{},
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("180")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}

	t.Run("env_only", func(t *testing.T) {
		s := s
		s.KillSwitchEnv = true
		d := policy.Evaluate(i, s, c)
		if d.StrictOutcome != policy.OutcomeDeny {
			t.Fatalf("strict outcome = %v", d.StrictOutcome)
		}
		found := false
		for _, v := range d.Violations {
			if v.Code == policy.RuleKillSwitch {
				found = true
			}
		}
		if !found {
			t.Fatal("expected KILL_SWITCH violation")
		}
	})

	t.Run("db_only", func(t *testing.T) {
		s := s
		s.KillSwitchEnv = false
		s.KillSwitchDB = true
		d := policy.Evaluate(i, s, c)
		if !containsCode(d.Violations, policy.RuleKillSwitch) {
			t.Fatal("expected KILL_SWITCH")
		}
	})

	t.Run("both_OR", func(t *testing.T) {
		s := s
		s.KillSwitchEnv = true
		s.KillSwitchDB = true
		d := policy.Evaluate(i, s, c)
		if !containsCode(d.Violations, policy.RuleKillSwitch) {
			t.Fatal("expected KILL_SWITCH")
		}
	})

	t.Run("off", func(t *testing.T) {
		s := s
		s.KillSwitchEnv = false
		s.KillSwitchDB = false
		d := policy.Evaluate(i, s, c)
		if containsCode(d.Violations, policy.RuleKillSwitch) {
			t.Fatal("unexpected KILL_SWITCH")
		}
	})
}

func TestEvaluate_data_mark_price_and_equity(t *testing.T) {
	c := baseCfg()
	sBase := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		PositionQtyBySymbol: map[string]decimal.Decimal{},
		MarkPriceBySymbol:   map[string]decimal.Decimal{},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("10"),
		OrderType:   "market",
		TimeInForce: "day",
	}

	d := policy.Evaluate(i, sBase, c)
	if !containsCode(d.Violations, policy.RuleDataMarkPrice) {
		t.Fatalf("want DATA_MARK_PRICE, got %v", policy.CompactViolationSummary(d.Violations))
	}

	s2 := sBase
	s2.MarkPriceBySymbol = map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")}
	s2.PortfolioEquity = decimal.Zero
	d2 := policy.Evaluate(i, s2, c)
	if !containsCode(d2.Violations, policy.RuleDataEquity) {
		t.Fatalf("want DATA_EQUITY_INVALID: %v", policy.CompactViolationSummary(d2.Violations))
	}
}

func TestEvaluate_whitelist_blacklist(t *testing.T) {
	c := baseCfg()
	c.SymbolWhitelist = []string{"MSFT"}
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	d := policy.Evaluate(i, s, c)
	if !containsCode(d.Violations, policy.RuleSymbolWhitelist) {
		t.Fatal("expected whitelist violation")
	}

	c2 := baseCfg()
	c2.SymbolBlacklist = []string{"AAPL"}
	d2 := policy.Evaluate(i, s, c2)
	if !containsCode(d2.Violations, policy.RuleSymbolBlacklist) {
		t.Fatal("expected blacklist violation")
	}
}

func TestEvaluate_daily_loss_anchor_and_limit(t *testing.T) {
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("90000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.Zero,
	}
	c := baseCfg()
	c.MaxDailyLossPct = decimal.RequireFromString("2")

	d := policy.Evaluate(i, s, c)
	if !containsCode(d.Violations, policy.RuleDailyLossAnchor) {
		t.Fatalf("want anchor missing: %v", policy.CompactViolationSummary(d.Violations))
	}

	s2 := s
	s2.EquityAnchor = decimal.RequireFromString("100000")
	s2.PortfolioEquity = decimal.RequireFromString("97000")
	d2 := policy.Evaluate(i, s2, c)
	if !containsCode(d2.Violations, policy.RuleDailyLossLimit) {
		t.Fatalf("want daily loss: %v", policy.CompactViolationSummary(d2.Violations))
	}
}

func TestEvaluate_max_notional_and_daily_sum(t *testing.T) {
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("100"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:      decimal.RequireFromString("1000000"),
		MarkPriceBySymbol:    map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("200")},
		NowNY:                wednesdaySession(t),
		EquityAnchor:         decimal.RequireFromString("1000000"),
		DailyNotionalUsedUSD: decimal.RequireFromString("0"),
	}
	c := baseCfg()
	c.MaxOrderNotionalUSD = decimal.RequireFromString("1000")

	d := policy.Evaluate(i, s, c)
	if !containsCode(d.Violations, policy.RuleMaxOrderNotional) {
		t.Fatalf("want max order notional: %v", policy.CompactViolationSummary(d.Violations))
	}

	i2 := i
	i2.Quantity = qty("1")
	c2 := baseCfg()
	c2.MaxDailyNotionalUSD = decimal.RequireFromString("150")
	s2 := s
	s2.DailyNotionalUsedUSD = decimal.RequireFromString("100")
	d2 := policy.Evaluate(i2, s2, c2)
	if !containsCode(d2.Violations, policy.RuleMaxDailyNotional) {
		t.Fatalf("want daily notional: %v", policy.CompactViolationSummary(d2.Violations))
	}
}

func TestEvaluate_max_position_pct_and_no_short(t *testing.T) {
	c := baseCfg()
	c.MaxPositionPct = decimal.RequireFromString("10")
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1000"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		PositionQtyBySymbol: map[string]decimal.Decimal{},
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("50")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	d := policy.Evaluate(i, s, c)
	if !containsCode(d.Violations, policy.RuleMaxPositionPct) {
		t.Fatalf("want position pct: %v", policy.CompactViolationSummary(d.Violations))
	}

	iSell := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideSell,
		Quantity:    qty("100"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	sShort := s
	sShort.PositionQtyBySymbol = map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("10")}
	dShort := policy.Evaluate(iSell, sShort, c)
	if !containsCode(dShort.Violations, policy.RuleNoShort) {
		t.Fatalf("want no short: %v", policy.CompactViolationSummary(dShort.Violations))
	}
}

func TestEvaluate_no_market_hours_gate_at_materialize(t *testing.T) {
	c := baseCfg()
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               saturday(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	d := policy.Evaluate(i, s, c)
	if containsCode(d.Violations, policy.RuleMarketHours) {
		t.Fatalf("materialize evaluate should not gate on MARKET_HOURS: %v", policy.CompactViolationSummary(d.Violations))
	}
	if d.StrictOutcome != policy.OutcomeAllow {
		t.Fatalf("want ALLOW outside regular session when other rules pass: %v", policy.CompactViolationSummary(d.Violations))
	}
}

func TestEvaluate_broker_blocks_and_PDT_stub(t *testing.T) {
	c := baseCfg()
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
		OptionalBroker: &policy.BrokerAccountSnapshot{
			TradingBlocked: true,
		},
	}
	d := policy.Evaluate(i, s, c)
	if !containsCode(d.Violations, policy.RuleTradingBlocked) {
		t.Fatal("expected TRADING_BLOCKED")
	}

	s2 := s
	s2.OptionalBroker = &policy.BrokerAccountSnapshot{AccountBlocked: true}
	d2 := policy.Evaluate(i, s2, c)
	if !containsCode(d2.Violations, policy.RuleAccountBlocked) {
		t.Fatal("expected ACCOUNT_BLOCKED")
	}

	s3 := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
		OptionalBroker: &policy.BrokerAccountSnapshot{
			PatternDayTrader: true,
			Equity:           decimal.RequireFromString("20000"),
		},
	}
	d3 := policy.Evaluate(i, s3, c)
	if !containsCode(d3.Violations, policy.RulePDTUnder25k) {
		t.Fatalf("want PDT stub: %v", policy.CompactViolationSummary(d3.Violations))
	}

	iSell := i
	iSell.Side = domain.SideSell
	d4 := policy.Evaluate(iSell, s3, c)
	if containsCode(d4.Violations, policy.RulePDTUnder25k) {
		t.Fatal("PDT stub should not apply to SELL")
	}
}

func TestEvaluate_rate_limit(t *testing.T) {
	c := baseCfg()
	c.MaxOrdersPerMinute = 2
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
		OrdersLastMinute:    2,
	}
	d := policy.Evaluate(i, s, c)
	if !containsCode(d.Violations, policy.RuleRateLimit) {
		t.Fatalf("want RATE_LIMIT: %v", policy.CompactViolationSummary(d.Violations))
	}
}

func TestEvaluate_monitor_mode(t *testing.T) {
	c := baseCfg()
	c.Mode = policy.ModeMonitor
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		KillSwitchEnv:       true,
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	d := policy.Evaluate(i, s, c)
	if d.StrictOutcome != policy.OutcomeDeny {
		t.Fatalf("strict should deny, got %v", d.StrictOutcome)
	}
	if d.EffectiveOutcome != policy.OutcomeAllow {
		t.Fatalf("effective should ALLOW in monitor, got %v", d.EffectiveOutcome)
	}
	if len(d.Violations) == 0 {
		t.Fatal("expected violations recorded for audit")
	}
}

func TestEvaluate_rule_ordering_smoke(t *testing.T) {
	c := baseCfg()
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		KillSwitchEnv:       true,
		PortfolioEquity:     decimal.RequireFromString("100000"),
		MarkPriceBySymbol:   map[string]decimal.Decimal{}, // missing mark also
		NowNY:               saturday(t),                   // market closed also
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	d := policy.Evaluate(i, s, c)
	codes := violationCodes(d.Violations)
	if len(codes) < 2 {
		t.Fatalf("expected multiple violations, got %v", codes)
	}
	if codes[0] != policy.RuleKillSwitch {
		t.Fatalf("first rule should be KILL_SWITCH, got %v", codes[0])
	}
}

func TestPolicyConfigHash_stable(t *testing.T) {
	c1 := baseCfg()
	c1.SymbolWhitelist = []string{"B", "A"}
	c2 := baseCfg()
	c2.SymbolWhitelist = []string{"A", "B"}
	if policy.PolicyConfigHash(c1) != policy.PolicyConfigHash(c2) {
		t.Fatal("whitelist order should not affect config hash")
	}
}

func TestEvaluateForBrokerSubmit_waives_soft_risk_limits(t *testing.T) {
	c := baseCfg()
	c.MaxPositionPct = decimal.RequireFromString("10")
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1000"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		PositionQtyBySymbol: map[string]decimal.Decimal{},
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("50")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	full := policy.Evaluate(i, s, c)
	if full.StrictOutcome != policy.OutcomeDeny || !containsCode(full.Violations, policy.RuleMaxPositionPct) {
		t.Fatalf("full evaluate should deny on position pct: %v", policy.CompactViolationSummary(full.Violations))
	}
	br := policy.EvaluateForBrokerSubmit(i, s, c)
	if br.StrictOutcome != policy.OutcomeAllow || len(br.Violations) != 0 {
		t.Fatalf("broker submit should allow (no hard gates): violations=%v", policy.CompactViolationSummary(br.Violations))
	}
}

func TestEvaluateForBrokerSubmit_allows_outside_US_regular_session(t *testing.T) {
	c := baseCfg()
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		PositionQtyBySymbol: map[string]decimal.Decimal{},
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               saturday(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
	}
	full := policy.Evaluate(i, s, c)
	if full.StrictOutcome != policy.OutcomeAllow {
		t.Fatalf("evaluate outside regular session should allow: %v", policy.CompactViolationSummary(full.Violations))
	}
	br := policy.EvaluateForBrokerSubmit(i, s, c)
	if br.StrictOutcome != policy.OutcomeAllow || len(br.Violations) != 0 {
		t.Fatalf("broker submit should allow: %v", policy.CompactViolationSummary(br.Violations))
	}
}

func TestEvaluateForBrokerSubmit_keeps_kill_switch(t *testing.T) {
	c := baseCfg()
	i := policy.Intent{
		Symbol:      "AAPL",
		Side:        domain.SideBuy,
		Quantity:    qty("1"),
		OrderType:   "market",
		TimeInForce: "day",
	}
	s := policy.Snapshot{
		PortfolioEquity:     decimal.RequireFromString("100000"),
		PositionQtyBySymbol: map[string]decimal.Decimal{},
		MarkPriceBySymbol:   map[string]decimal.Decimal{"AAPL": decimal.RequireFromString("100")},
		NowNY:               wednesdaySession(t),
		EquityAnchor:        decimal.RequireFromString("100000"),
		KillSwitchEnv:       true,
	}
	br := policy.EvaluateForBrokerSubmit(i, s, c)
	if br.EffectiveOutcome != policy.OutcomeDeny || !containsCode(br.Violations, policy.RuleKillSwitch) {
		t.Fatalf("broker submit should still block kill switch: %v", policy.CompactViolationSummary(br.Violations))
	}
}

func containsCode(vs []policy.Violation, code string) bool {
	for _, v := range vs {
		if v.Code == code {
			return true
		}
	}
	return false
}

func violationCodes(vs []policy.Violation) []string {
	out := make([]string, len(vs))
	for i := range vs {
		out[i] = vs[i].Code
	}
	return out
}
