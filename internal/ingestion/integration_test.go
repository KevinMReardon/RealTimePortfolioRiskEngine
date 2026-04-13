package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// ingestMemStore records appended events (integration test double).
type ingestMemStore struct {
	events []domain.EventEnvelope
}

func (m *ingestMemStore) Append(ctx context.Context, e domain.EventEnvelope) (events.AppendResult, error) {
	m.events = append(m.events, e)
	return events.AppendResult{EventID: e.EventID, Inserted: true}, nil
}

func tradeEnvelope(portfolio string, idem string, trade domain.TradePayload) domain.EventEnvelope {
	p, _ := json.Marshal(trade)
	return domain.EventEnvelope{
		EventID:        uuid.New(),
		EventType:      domain.EventTypeTradeExecuted,
		EventTime:      time.Now().UTC(),
		ProcessingTime: time.Now().UTC(),
		Source:         "trade_api",
		PortfolioID:    portfolio,
		IdempotencyKey: idem,
		Payload:        p,
	}
}

// TestIngestThenApply_ShortingUnderflow: ingest accepts shape-valid sells that would short;
// apply-time no-shorting is enforced in portfolio.Aggregate / domain.Positions.
func TestIngestThenApply_ShortingUnderflow(t *testing.T) {
	t.Parallel()
	store := &ingestMemStore{}
	svc := NewService(store)
	ctx := context.Background()
	pid := uuid.MustParse("550e8400-e29b-41d4-a716-446655440099").String()

	buy := domain.TradePayload{
		TradeID:  "b1",
		Symbol:   "AAPL",
		Side:     domain.SideBuy,
		Quantity: decimal.NewFromInt(5),
		Price:    decimal.NewFromInt(10),
		Currency: "USD",
	}
	if _, err := svc.Ingest(ctx, tradeEnvelope(pid, "idem-buy", buy)); err != nil {
		t.Fatalf("ingest buy: %v", err)
	}

	sellTooBig := domain.TradePayload{
		TradeID:  "s1",
		Symbol:   "AAPL",
		Side:     domain.SideSell,
		Quantity: decimal.NewFromInt(10),
		Price:    decimal.NewFromInt(11),
		Currency: "USD",
	}
	if _, err := svc.Ingest(ctx, tradeEnvelope(pid, "idem-sell", sellTooBig)); err != nil {
		t.Fatalf("ingest oversell (shape valid): %v", err)
	}

	agg := portfolio.NewAggregate()
	for i, ev := range store.events {
		err := agg.ApplyEvent(pid, ev)
		if i == 1 {
			if !errors.Is(err, domain.ErrPositionUnderflow) {
				t.Fatalf("step 1: want ErrPositionUnderflow, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}

	pos := domain.NewPositions()
	for i, ev := range store.events {
		var tr domain.TradePayload
		if err := json.Unmarshal(ev.Payload, &tr); err != nil {
			t.Fatal(err)
		}
		err := pos.ApplyTrade(tr)
		if i == 1 {
			if !errors.Is(err, domain.ErrPositionUnderflow) {
				t.Fatalf("positions step 1: want ErrPositionUnderflow, got %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("positions step %d: %v", i, err)
		}
	}
	if !pos.Quantity("AAPL").Equal(decimal.NewFromInt(5)) {
		t.Fatalf("qty after failed sell = %s want 5", pos.Quantity("AAPL"))
	}
}
