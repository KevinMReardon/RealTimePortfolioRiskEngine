package portfolio

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

func TestMarshalTradePositionsSnapshotV1_Empty(t *testing.T) {
	a := NewAggregate()
	pid := uuid.New().String()
	raw, err := MarshalTradePositionsSnapshotV1(a, pid)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["format"] != tradePositionsSnapshotFormatV1 {
		t.Fatalf("format: %v", got["format"])
	}
	pos, _ := got["positions"].([]any)
	if len(pos) != 0 {
		t.Fatalf("positions: %v", pos)
	}
}

func TestParseTradePositionsSnapshotV1_Roundtrip(t *testing.T) {
	a := NewAggregate()
	pid := uuid.New().String()
	_ = a.ApplyEvent(pid, tradeEv("k1", "AA", 3))
	raw, err := MarshalTradePositionsSnapshotV1(a, pid)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParseTradePositionsSnapshotV1(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Quantity("AA").Equal(decimal.NewFromInt(3)) {
		t.Fatalf("quantity got %s", p.Quantity("AA"))
	}
}

func TestParseTradePositionsSnapshotV1_WrongFormat(t *testing.T) {
	_, err := ParseTradePositionsSnapshotV1([]byte(`{"format":"v2","positions":[]}`))
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrUnsupportedPortfolioSnapshotFormat) {
		t.Fatalf("got %v", err)
	}
}

func TestMarshalTradePositionsSnapshotV1_SortsSymbols(t *testing.T) {
	a := NewAggregate()
	pid := uuid.New().String()
	_ = a.ApplyEvent(pid, tradeEv("k1", "ZZ", 1))
	_ = a.ApplyEvent(pid, tradeEv("k2", "AA", 2))
	raw, err := MarshalTradePositionsSnapshotV1(a, pid)
	if err != nil {
		t.Fatal(err)
	}
	var got tradePositionsSnapshotV1
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Positions) != 2 {
		t.Fatalf("len %d", len(got.Positions))
	}
	if got.Positions[0].Symbol != "AA" || got.Positions[1].Symbol != "ZZ" {
		t.Fatalf("order %+v", got.Positions)
	}
}

func tradeEv(idem, sym string, qty int64) domain.EventEnvelope {
	p, _ := json.Marshal(domain.TradePayload{
		TradeID:  idem,
		Symbol:   sym,
		Side:     domain.SideBuy,
		Quantity: decimal.NewFromInt(qty),
		Price:    decimal.NewFromInt(10),
		Currency: "USD",
	})
	return domain.EventEnvelope{
		EventID:        uuid.New(),
		EventType:      domain.EventTypeTradeExecuted,
		Source:         "test",
		PortfolioID:    "",
		IdempotencyKey: idem,
		Payload:        p,
	}
}
