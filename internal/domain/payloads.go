package domain

import "github.com/shopspring/decimal"

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
}
