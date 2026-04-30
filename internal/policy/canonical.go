package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

type inputsHashPayload struct {
	Intent struct {
		Symbol      string  `json:"symbol"`
		Side        string  `json:"side"`
		Quantity    *string `json:"quantity,omitempty"`
		NotionalUSD *string `json:"notional_usd,omitempty"`
		OrderType   string  `json:"order_type"`
		TimeInForce string  `json:"time_in_force"`
		LimitPrice  *string `json:"limit_price,omitempty"`
	} `json:"intent"`
	Snapshot struct {
		PortfolioEquity        string            `json:"portfolio_equity"`
		PositionQtyBySymbol    map[string]string `json:"position_qty_by_symbol"`
		MarkPriceBySymbol      map[string]string `json:"mark_price_by_symbol"`
		NowNYRFC3339Nano       string            `json:"now_ny_rfc3339nano"`
		EquityAnchor           string            `json:"equity_anchor"`
		KillSwitchEnv          bool              `json:"kill_switch_env"`
		KillSwitchDB           bool              `json:"kill_switch_db"`
		DailyNotionalUsedUSD   string            `json:"daily_notional_used_usd"`
		OrdersLastMinute       int               `json:"orders_last_minute"`
		BrokerPatternDayTrader *bool             `json:"broker_pattern_day_trader,omitempty"`
		BrokerTradingBlocked   *bool             `json:"broker_trading_blocked,omitempty"`
		BrokerAccountBlocked   *bool             `json:"broker_account_blocked,omitempty"`
		BrokerEquity           *string           `json:"broker_equity,omitempty"`
	} `json:"snapshot"`
}

func decimalPtrString(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.StringFixed(12)
	return &s
}

func decimalString(d decimal.Decimal) string {
	return d.StringFixed(12)
}

// InputsHash returns SHA-256 hex of canonical intent+snapshot fields for replay (proposed_trades.policy_inputs_hash companion).
func InputsHash(intent Intent, snap Snapshot) string {
	raw := CanonicalInputsBytes(intent, snap)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// CanonicalInputsBytes returns JSON bytes used for InputsHash.
func CanonicalInputsBytes(intent Intent, snap Snapshot) []byte {
	var p inputsHashPayload
	p.Intent.Symbol = NormalizeSymbol(intent.Symbol)
	p.Intent.Side = string(intent.Side)
	p.Intent.Quantity = decimalPtrString(intent.Quantity)
	p.Intent.NotionalUSD = decimalPtrString(intent.NotionalUSD)
	p.Intent.OrderType = intent.OrderType
	p.Intent.TimeInForce = intent.TimeInForce
	p.Intent.LimitPrice = decimalPtrString(intent.LimitPrice)

	p.Snapshot.PortfolioEquity = decimalString(snap.PortfolioEquity)
	p.Snapshot.NowNYRFC3339Nano = snap.NowNY.Format(time.RFC3339Nano)
	p.Snapshot.EquityAnchor = decimalString(snap.EquityAnchor)
	p.Snapshot.KillSwitchEnv = snap.KillSwitchEnv
	p.Snapshot.KillSwitchDB = snap.KillSwitchDB
	p.Snapshot.DailyNotionalUsedUSD = decimalString(snap.DailyNotionalUsedUSD)
	p.Snapshot.OrdersLastMinute = snap.OrdersLastMinute

	keys := make([]string, 0, len(snap.PositionQtyBySymbol))
	for k := range snap.PositionQtyBySymbol {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	p.Snapshot.PositionQtyBySymbol = make(map[string]string, len(keys))
	for _, k := range keys {
		p.Snapshot.PositionQtyBySymbol[k] = snap.PositionQtyBySymbol[k].StringFixed(12)
	}

	mkeys := make([]string, 0, len(snap.MarkPriceBySymbol))
	for k := range snap.MarkPriceBySymbol {
		mkeys = append(mkeys, k)
	}
	sort.Strings(mkeys)
	p.Snapshot.MarkPriceBySymbol = make(map[string]string, len(mkeys))
	for _, k := range mkeys {
		p.Snapshot.MarkPriceBySymbol[k] = snap.MarkPriceBySymbol[k].StringFixed(12)
	}

	if snap.OptionalBroker != nil {
		pdt := snap.OptionalBroker.PatternDayTrader
		tb := snap.OptionalBroker.TradingBlocked
		ab := snap.OptionalBroker.AccountBlocked
		eq := snap.OptionalBroker.Equity.StringFixed(12)
		p.Snapshot.BrokerPatternDayTrader = &pdt
		p.Snapshot.BrokerTradingBlocked = &tb
		p.Snapshot.BrokerAccountBlocked = &ab
		p.Snapshot.BrokerEquity = &eq
	}

	raw, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return raw
}

type orderIntentPayload struct {
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	Quantity    *string `json:"quantity,omitempty"`
	NotionalUSD *string `json:"notional_usd,omitempty"`
	OrderType   string  `json:"order_type,omitempty"`
	TimeInForce string  `json:"time_in_force,omitempty"`
	LimitPrice  *string `json:"limit_price,omitempty"`
}

// CanonicalOrderIntentBytes is stable JSON for the executable order leg only (approve/deny binding).
func CanonicalOrderIntentBytes(intent Intent) []byte {
	var p orderIntentPayload
	p.Symbol = NormalizeSymbol(intent.Symbol)
	p.Side = string(intent.Side)
	p.Quantity = decimalPtrString(intent.Quantity)
	p.NotionalUSD = decimalPtrString(intent.NotionalUSD)
	p.OrderType = intent.OrderType
	p.TimeInForce = intent.TimeInForce
	p.LimitPrice = decimalPtrString(intent.LimitPrice)
	raw, err := json.Marshal(p)
	if err != nil {
		return nil
	}
	return raw
}

// OrderPayloadHash is SHA-256 hex of CanonicalOrderIntentBytes (proposed_trades.payload_hash).
func OrderPayloadHash(intent Intent) string {
	raw := CanonicalOrderIntentBytes(intent)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
