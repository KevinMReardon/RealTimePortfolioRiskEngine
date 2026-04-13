package domain

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ValidateSymbol ensures symbol matches v1 domain ticker regex (LLD symbol/universe policy).
func ValidateSymbol(symbol string) error {
	if !IsValidSymbol(symbol) {
		return fmt.Errorf("%w: invalid symbol", ErrValidation)
	}
	return nil
}

// ValidateQuantity requires trade quantity > 0 (LLD §4.2).
func ValidateQuantity(qty decimal.Decimal) error {
	if !qty.IsPositive() {
		return fmt.Errorf("%w: quantity must be > 0", ErrValidation)
	}
	return nil
}

// ValidatePrice requires price > 0 for trades and price updates (LLD §4.2, §4.3).
func ValidatePrice(price decimal.Decimal) error {
	if !price.IsPositive() {
		return fmt.Errorf("%w: price must be > 0", ErrValidation)
	}
	return nil
}

// ValidateSide requires side BUY or SELL (LLD §4.2 JSON strings).
func ValidateSide(side Side) error {
	if !IsValidSide(side) {
		return fmt.Errorf("%w: invalid side", ErrValidation)
	}
	return nil
}

// ValidateTradePayload checks TradeExecuted shape before append: trade_id, symbol,
// side, positive quantity and price, currency (LLD §4.2). It does not enforce no-shorting;
// use Positions.ApplyTrade at apply time for that.
func ValidateTradePayload(t TradePayload) error {
	if t.TradeID == "" {
		return fmt.Errorf("%w: trade_id required", ErrValidation)
	}
	if err := ValidateSymbol(t.Symbol); err != nil {
		return err
	}
	if err := ValidateSide(t.Side); err != nil {
		return err
	}
	if err := ValidateQuantity(t.Quantity); err != nil {
		return err
	}
	if err := ValidatePrice(t.Price); err != nil {
		return err
	}
	if t.Currency == "" {
		return fmt.Errorf("%w: currency required", ErrValidation)
	}
	return nil
}

// ValidatePricePayload checks PriceUpdated shape before append (LLD §4.3).
func ValidatePricePayload(p PricePayload) error {
	if err := ValidateSymbol(p.Symbol); err != nil {
		return err
	}
	if err := ValidatePrice(p.Price); err != nil {
		return err
	}
	if p.Currency == "" {
		return fmt.Errorf("%w: currency required", ErrValidation)
	}
	return nil
}

// ValidateEventEnvelope checks envelope metadata (LLD §4.1): non-nil event_id, non-zero
// event_time, portfolio_id, idempotency_key, source, non-empty payload JSON, and
// event_type TradeExecuted or PriceUpdated. It does not decode payload fields; use
// ValidateTradePayload / ValidatePricePayload after unmarshaling by type.
func ValidateEventEnvelope(e EventEnvelope) error {
	if e.EventID == uuid.Nil {
		return fmt.Errorf("%w: event_id required", ErrValidation)
	}
	if e.EventTime.IsZero() {
		return fmt.Errorf("%w: event_time required", ErrValidation)
	}
	if e.PortfolioID == "" {
		return fmt.Errorf("%w: portfolio_id required", ErrValidation)
	}
	if e.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency_key required", ErrValidation)
	}
	if e.Source == "" {
		return fmt.Errorf("%w: source required", ErrValidation)
	}
	if len(e.Payload) == 0 {
		return fmt.Errorf("%w: payload required", ErrValidation)
	}

	switch e.EventType {
	case EventTypeTradeExecuted, EventTypePriceUpdated:
	default:
		return fmt.Errorf("%w: invalid event_type", ErrValidation)
	}

	// Basic JSON well-formedness check; we don't decode into a concrete
	// payload type here to keep this function generic.
	var tmp json.RawMessage
	if err := json.Unmarshal(e.Payload, &tmp); err != nil {
		return fmt.Errorf("%w: payload must be valid JSON", ErrValidation)
	}

	return nil
}

