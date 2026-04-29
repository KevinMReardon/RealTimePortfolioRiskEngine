// Package policy implements deterministic policy-as-code evaluation for proposed trades (no LLM).
package policy

import (
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/shopspring/decimal"
)

// Mode controls whether rule violations block the proposal or are logged only.
type Mode string

const (
	ModeEnforce Mode = "enforce"
	ModeMonitor Mode = "monitor"
)

// Outcome is the strict rule outcome before monitor-mode relaxation.
type Outcome string

const (
	OutcomeAllow Outcome = "ALLOW"
	OutcomeDeny  Outcome = "DENY"
)

// Intent is a structured proposed order for evaluation.
type Intent struct {
	Symbol      string
	Side        domain.Side
	Quantity    *decimal.Decimal // optional when NotionalUSD set
	NotionalUSD *decimal.Decimal // optional when Quantity set
	OrderType   string
	TimeInForce string
	LimitPrice  *decimal.Decimal
}

// BrokerAccountSnapshot mirrors Alpaca account flags relevant to PDT / blocks (subset of connectors/alpaca.AccountSummary).
type BrokerAccountSnapshot struct {
	PatternDayTrader bool
	TradingBlocked   bool
	AccountBlocked   bool
	// Equity is Alpaca account equity (used with PatternDayTrader for under-$25k stub).
	Equity decimal.Decimal
}

// Snapshot is replayable inputs for Evaluate (filled by the caller; no DB I/O in this package).
type Snapshot struct {
	PortfolioEquity decimal.Decimal
	// PositionQtyBySymbol is long quantity per symbol before this trade (non-negative).
	PositionQtyBySymbol map[string]decimal.Decimal
	MarkPriceBySymbol   map[string]decimal.Decimal
	// NowNY is the evaluation instant in America/New_York (caller should pass clock in that TZ or convert).
	NowNY time.Time

	// EquityAnchor is portfolio equity at NY calendar day start for daily loss (zero means anchor unavailable).
	EquityAnchor decimal.Decimal

	OptionalBroker *BrokerAccountSnapshot

	KillSwitchEnv bool
	KillSwitchDB  bool

	// DailyNotionalUsedUSD is cumulative notional already committed today (USD) before this trade.
	DailyNotionalUsedUSD decimal.Decimal
	// OrdersLastMinute is count of orders in the sliding window used for rate limiting.
	OrdersLastMinute int
}

// Violation records one failed rule for audit (monitor mode collects all applicable violations).
type Violation struct {
	Code   string
	Detail string
}

// Decision is the result of Evaluate.
type Decision struct {
	Violations []Violation

	// StrictOutcome is DENY if any violation was recorded (what enforce mode applies logically).
	StrictOutcome Outcome

	// EffectiveOutcome applies Mode: enforce → same as StrictOutcome; monitor → ALLOW if violations only affect audit.
	EffectiveOutcome Outcome

	PolicyConfigHash string
	InputsHash       string
}

// Rule codes (stable identifiers for metrics / audit).
const (
	RuleKillSwitch           = "KILL_SWITCH"
	RuleDataMarkPrice        = "DATA_MARK_PRICE"
	RuleDataEquity           = "DATA_EQUITY_INVALID"
	RuleSymbolWhitelist      = "SYMBOL_WHITELIST"
	RuleSymbolBlacklist      = "SYMBOL_BLACKLIST"
	RuleDailyLossAnchor      = "DAILY_LOSS_ANCHOR_MISSING"
	RuleDailyLossLimit       = "DAILY_LOSS_LIMIT"
	RuleMaxOrderNotional     = "MAX_ORDER_NOTIONAL"
	RuleMaxDailyNotional     = "MAX_DAILY_NOTIONAL"
	RuleMaxPositionPct       = "MAX_POSITION_PCT"
	RuleNoShort              = "NO_SHORT_SELL"
	RuleMarketHours          = "MARKET_HOURS"
	RuleTradingBlocked       = "TRADING_BLOCKED"
	RuleAccountBlocked       = "ACCOUNT_BLOCKED"
	RulePDTUnder25k          = "PDT_UNDER_25K_BUY"
	RuleRateLimit            = "RATE_LIMIT_ORDER"
	RuleIntentSymbol = "INTENT_SYMBOL_INVALID"
	RuleIntentSide   = "INTENT_SIDE_INVALID"
)
