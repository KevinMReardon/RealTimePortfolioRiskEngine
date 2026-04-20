package alpaca

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
)

// dedupAppender records first append per idempotency key; repeats return Inserted=false with same EventID.
type dedupAppender struct {
	mu   sync.Mutex
	seen map[string]uuid.UUID
}

func (d *dedupAppender) Append(_ context.Context, e domain.EventEnvelope) (events.AppendResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.seen == nil {
		d.seen = make(map[string]uuid.UUID)
	}
	if prev, ok := d.seen[e.IdempotencyKey]; ok {
		return events.AppendResult{EventID: prev, Inserted: false}, nil
	}
	d.seen[e.IdempotencyKey] = e.EventID
	return events.AppendResult{EventID: e.EventID, Inserted: true}, nil
}

func TestActivityToTradePayload_mapping(t *testing.T) {
	t.Parallel()
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		act     ActivityRow
		want    domain.TradePayload
		wantErr bool
	}{
		{
			name: "buy_uppercases_symbol",
			act: ActivityRow{
				ID:              "act-1",
				ActivityType:    "FILL",
				TransactionTime: ts,
				Symbol:          "aapl",
				Qty:             decimal.NewFromInt(5),
				Price:           decimal.RequireFromString("150.25"),
				Side:            "buy",
			},
			want: domain.TradePayload{
				TradeID:  "act-1",
				Symbol:   "AAPL",
				Side:     domain.SideBuy,
				Quantity: decimal.NewFromInt(5),
				Price:    decimal.RequireFromString("150.25"),
				Currency: "USD",
			},
		},
		{
			name: "sell",
			act: ActivityRow{
				ID:              "act-2",
				ActivityType:    "FILL",
				TransactionTime: ts,
				Symbol:          "MSFT",
				Qty:             decimal.NewFromInt(2),
				Price:           decimal.NewFromInt(300),
				Side:            "SELL",
			},
			want: domain.TradePayload{
				TradeID:  "act-2",
				Symbol:   "MSFT",
				Side:     domain.SideSell,
				Quantity: decimal.NewFromInt(2),
				Price:    decimal.NewFromInt(300),
				Currency: "USD",
			},
		},
		{
			name: "negative_qty_abs",
			act: ActivityRow{
				ID:              "act-3",
				ActivityType:    "FILL",
				TransactionTime: ts,
				Symbol:          "XYZ",
				Qty:             decimal.NewFromInt(-10),
				Price:           decimal.NewFromInt(1),
				Side:            "buy",
			},
			want: domain.TradePayload{
				TradeID:  "act-3",
				Symbol:   "XYZ",
				Side:     domain.SideBuy,
				Quantity: decimal.NewFromInt(10),
				Price:    decimal.NewFromInt(1),
				Currency: "USD",
			},
		},
		{name: "empty_symbol", act: ActivityRow{ID: "x", ActivityType: "FILL", TransactionTime: ts, Symbol: "  ", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(1), Side: "buy"}, wantErr: true},
		{name: "empty_id", act: ActivityRow{ID: "", ActivityType: "FILL", TransactionTime: ts, Symbol: "A", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(1), Side: "buy"}, wantErr: true},
		{name: "zero_qty", act: ActivityRow{ID: "z", ActivityType: "FILL", TransactionTime: ts, Symbol: "A", Qty: decimal.Zero, Price: decimal.NewFromInt(1), Side: "buy"}, wantErr: true},
		{name: "bad_side", act: ActivityRow{ID: "z", ActivityType: "FILL", TransactionTime: ts, Symbol: "A", Qty: decimal.NewFromInt(1), Price: decimal.NewFromInt(1), Side: "HOLD"}, wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ActivityToTradePayload(tc.act)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !tc.want.Quantity.Equal(got.Quantity) {
				t.Fatalf("quantity: want %s got %s", tc.want.Quantity, got.Quantity)
			}
			if !tc.want.Price.Equal(got.Price) {
				t.Fatalf("price: want %s got %s", tc.want.Price, got.Price)
			}
			tc.want.Quantity = got.Quantity
			tc.want.Price = got.Price
			if got != tc.want {
				t.Fatalf("payload: want %+v got %+v", tc.want, got)
			}
		})
	}
}

func TestTryIngestFillActivity_skips_non_fill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dedup := &dedupAppender{}
	svc := ingestion.NewService(dedup)
	pid := uuid.New()
	act := ActivityRow{
		ID:              "div-1",
		ActivityType:    "DIV",
		TransactionTime: time.Now().UTC(),
		Symbol:          "AAPL",
		Qty:             decimal.NewFromInt(1),
		Price:           decimal.NewFromInt(1),
		Side:            "buy",
	}
	out, err := TryIngestFillActivity(ctx, svc, pid, act)
	if err != nil {
		t.Fatal(err)
	}
	if out != OutcomeSkippedInvalid {
		t.Fatalf("out=%v want SkippedInvalid", out)
	}
	if len(dedup.seen) != 0 {
		t.Fatalf("dedup.seen=%v want empty", dedup.seen)
	}
}

func TestTryIngestFillActivity_duplicate_idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dedup := &dedupAppender{}
	svc := ingestion.NewService(dedup)
	pid := uuid.New()
	act := ActivityRow{
		ID:              "idem-key-1",
		ActivityType:    "FILL",
		TransactionTime: time.Now().UTC(),
		Symbol:          "AAPL",
		Qty:             decimal.NewFromInt(3),
		Price:           decimal.NewFromInt(100),
		Side:            "buy",
	}

	o1, err := TryIngestFillActivity(ctx, svc, pid, act)
	if err != nil {
		t.Fatal(err)
	}
	if o1 != OutcomeAppended {
		t.Fatalf("first out=%v want Appended", o1)
	}

	o2, err := TryIngestFillActivity(ctx, svc, pid, act)
	if err != nil {
		t.Fatal(err)
	}
	if o2 != OutcomeDuplicate {
		t.Fatalf("second out=%v want Duplicate", o2)
	}

	if len(dedup.seen) != 1 {
		t.Fatalf("seen keys: %d want 1", len(dedup.seen))
	}
}
