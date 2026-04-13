package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestValidateSymbol(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		symbol  string
		wantErr bool
	}{
		{"empty", "", true},
		{"lowercase_ticker", "aapl", true},
		{"mixed_case", "AaPL", true},
		{"too_long", strings.Repeat("A", 33), true},
		{"single_letter", "A", false},
		{"multi_letter", "AAPL", false},
		{"with_digit", "BRK1", false},
		{"with_dot", "BRK.B", false},
		{"with_underscore", "TICK_ER", false},
		{"with_hyphen", "ABC-DEF", false},
		{"spaces", "AA PL", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSymbol(tt.symbol)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, ErrValidation) {
					t.Fatalf("want ErrValidation, got %v", err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateQuantity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		qty     decimal.Decimal
		wantErr bool
	}{
		{"negative", decimal.NewFromInt(-1), true},
		{"zero", decimal.Zero, true},
		{"positive_one", decimal.NewFromInt(1), false},
		{"positive_fraction", decimal.RequireFromString("0.25"), false},
		{"large", decimal.RequireFromString("9999999999999999999999999999.99999999"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateQuantity(tt.qty)
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidateQuantity(%v): wantErr=%v err=%v", tt.qty, tt.wantErr, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}

func TestValidatePrice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		price   decimal.Decimal
		wantErr bool
	}{
		{"negative", decimal.NewFromInt(-1), true},
		{"zero", decimal.Zero, true},
		{"tiny_positive", decimal.RequireFromString("0.00000001"), false},
		{"normal", decimal.RequireFromString("180.25"), false},
		{"large", decimal.RequireFromString("1e20"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePrice(tt.price)
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidatePrice(%v): wantErr=%v err=%v", tt.price, tt.wantErr, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}

func TestValidateSide(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		side    Side
		wantErr bool
	}{
		{"buy", SideBuy, false},
		{"sell", SideSell, false},
		{"empty", "", true},
		{"lowercase_buy", "buy", true},
		{"hold", "HOLD", true},
		{"garbage", "BUY ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSide(tt.side)
			if tt.wantErr != (err != nil) {
				t.Fatalf("ValidateSide(%q): wantErr=%v err=%v", tt.side, tt.wantErr, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}

func validTradePayload() TradePayload {
	return TradePayload{
		TradeID:  "t-1",
		Symbol:   "AAPL",
		Side:     SideBuy,
		Quantity: decimal.NewFromInt(10),
		Price:    decimal.RequireFromString("100.50"),
		Currency: "USD",
	}
}

func validPricePayload() PricePayload {
	return PricePayload{
		Symbol:         "AAPL",
		Price:          decimal.RequireFromString("181.75"),
		Currency:       "USD",
		SourceSequence: 1,
	}
}

func validEventEnvelope(payload json.RawMessage) EventEnvelope {
	return EventEnvelope{
		EventID:        uuid.MustParse("3f2504e0-4f89-11d3-9a0c-0305e82c3301"),
		EventType:      EventTypeTradeExecuted,
		EventTime:      time.Date(2026, 3, 19, 14, 5, 30, 123000000, time.UTC),
		ProcessingTime: time.Date(2026, 3, 19, 14, 5, 30, 200000000, time.UTC),
		Source:         "trade_api",
		PortfolioID:    "550e8400-e29b-41d4-a716-446655440000",
		IdempotencyKey: "idem-1",
		Payload:        payload,
	}
}

func TestValidateTradePayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(TradePayload) TradePayload
		wantErr bool
	}{
		{"valid", func(p TradePayload) TradePayload { return p }, false},
		{"missing_trade_id", func(p TradePayload) TradePayload { p.TradeID = ""; return p }, true},
		{"bad_symbol", func(p TradePayload) TradePayload { p.Symbol = "bad"; return p }, true},
		{"bad_side", func(p TradePayload) TradePayload { p.Side = "LONG"; return p }, true},
		{"zero_qty", func(p TradePayload) TradePayload { p.Quantity = decimal.Zero; return p }, true},
		{"negative_qty", func(p TradePayload) TradePayload { p.Quantity = decimal.NewFromInt(-1); return p }, true},
		{"zero_price", func(p TradePayload) TradePayload { p.Price = decimal.Zero; return p }, true},
		{"missing_currency", func(p TradePayload) TradePayload { p.Currency = ""; return p }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.mutate(validTradePayload())
			err := ValidateTradePayload(p)
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v err=%v", tt.wantErr, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}

func TestValidatePricePayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(PricePayload) PricePayload
		wantErr bool
	}{
		{"valid", func(p PricePayload) PricePayload { return p }, false},
		{"bad_symbol", func(p PricePayload) PricePayload { p.Symbol = ""; return p }, true},
		{"zero_price", func(p PricePayload) PricePayload { p.Price = decimal.Zero; return p }, true},
		{"missing_currency", func(p PricePayload) PricePayload { p.Currency = ""; return p }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.mutate(validPricePayload())
			err := ValidatePricePayload(p)
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v err=%v", tt.wantErr, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}

func TestValidateEventEnvelope(t *testing.T) {
	t.Parallel()
	goodPayload := json.RawMessage(`{"ok":true}`)

	tests := []struct {
		name    string
		mutate  func(EventEnvelope) EventEnvelope
		wantErr bool
	}{
		{
			"valid_trade",
			func(e EventEnvelope) EventEnvelope {
				e.EventType = EventTypeTradeExecuted
				e.Payload = goodPayload
				return e
			},
			false,
		},
		{
			"valid_price",
			func(e EventEnvelope) EventEnvelope {
				e.EventType = EventTypePriceUpdated
				e.Payload = goodPayload
				return e
			},
			false,
		},
		{
			"nil_event_id",
			func(e EventEnvelope) EventEnvelope {
				e.EventID = uuid.Nil
				e.EventType = EventTypeTradeExecuted
				e.Payload = goodPayload
				return e
			},
			true,
		},
		{
			"zero_event_time",
			func(e EventEnvelope) EventEnvelope {
				e.EventTime = time.Time{}
				e.EventType = EventTypeTradeExecuted
				e.Payload = goodPayload
				return e
			},
			true,
		},
		{
			"empty_portfolio",
			func(e EventEnvelope) EventEnvelope {
				e.PortfolioID = ""
				e.EventType = EventTypeTradeExecuted
				e.Payload = goodPayload
				return e
			},
			true,
		},
		{
			"empty_idempotency",
			func(e EventEnvelope) EventEnvelope {
				e.IdempotencyKey = ""
				e.EventType = EventTypeTradeExecuted
				e.Payload = goodPayload
				return e
			},
			true,
		},
		{
			"empty_source",
			func(e EventEnvelope) EventEnvelope {
				e.Source = ""
				e.EventType = EventTypeTradeExecuted
				e.Payload = goodPayload
				return e
			},
			true,
		},
		{
			"empty_payload",
			func(e EventEnvelope) EventEnvelope {
				e.EventType = EventTypeTradeExecuted
				e.Payload = nil
				return e
			},
			true,
		},
		{
			"invalid_event_type",
			func(e EventEnvelope) EventEnvelope {
				e.EventType = "Unknown"
				e.Payload = goodPayload
				return e
			},
			true,
		},
		{
			"invalid_json_payload",
			func(e EventEnvelope) EventEnvelope {
				e.EventType = EventTypeTradeExecuted
				e.Payload = json.RawMessage(`{`)
				return e
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := validEventEnvelope(goodPayload)
			e := tt.mutate(base)
			err := ValidateEventEnvelope(e)
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v err=%v", tt.wantErr, err)
			}
			if err != nil && !errors.Is(err, ErrValidation) {
				t.Fatalf("want ErrValidation, got %v", err)
			}
		})
	}
}
