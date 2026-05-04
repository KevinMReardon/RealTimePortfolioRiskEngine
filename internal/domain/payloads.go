package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// TradePayload is the TradeExecuted body (LLD §4.2): trade_id, symbol, side, quantity,
// price, currency. quantity and price must be > 0 at validation time; SELL additionally
// must not drive position quantity negative at apply time (see Positions.ApplyTrade).
type TradePayload struct {
	TradeID  string          `json:"trade_id"`
	Symbol   string          `json:"symbol"`
	Side     Side            `json:"side"`
	Quantity decimal.Decimal `json:"quantity"`
	Price    decimal.Decimal `json:"price"`
	Currency string          `json:"currency"`
}

// PricePayload is the PriceUpdated body (LLD §4.3): symbol, price, currency,
// source_sequence. price must be > 0; symbol non-empty and matching symbol policy.
type PricePayload struct {
	Symbol         string          `json:"symbol"`
	Price          decimal.Decimal `json:"price"`
	Currency       string          `json:"currency"`
	SourceSequence int64           `json:"source_sequence"`
	// AsOf is the provider quote timestamp (omitted in legacy events). The automated
	// price feed sets this and uses wall-clock EventEnvelope.EventTime for stream
	// ordering so a quote is never inserted "behind" the projection cursor on its shard.
	AsOf time.Time `json:"as_of,omitempty"`
}

// MarkAsOfTime selects the instant used for prices_projection.as_of and returns.
// When AsOf is zero, the envelope's event_time is used.
func (p PricePayload) MarkAsOfTime(fallback time.Time) time.Time {
	if !p.AsOf.IsZero() {
		return p.AsOf.UTC()
	}
	return fallback.UTC()
}
