package pricefeed

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/pricesource"
)

type ingestRecorder struct {
	events []domain.EventEnvelope
}

func (r *ingestRecorder) Ingest(_ context.Context, event domain.EventEnvelope) (events.AppendResult, error) {
	r.events = append(r.events, event)
	return events.AppendResult{EventID: event.EventID, Inserted: true}, nil
}

type fakeProvider struct {
	name  string
	quote pricesource.PriceQuote
	err   error
}

func (p *fakeProvider) Name() string { return p.name }
func (p *fakeProvider) Health() pricesource.HealthMetadata {
	return pricesource.HealthMetadata{Provider: p.name, Healthy: p.err == nil}
}
func (p *fakeProvider) FetchQuotes(_ context.Context, _ []string) (pricesource.FetchResult, error) {
	if p.err != nil {
		return pricesource.FetchResult{}, p.err
	}
	return pricesource.FetchResult{
		Provider:  p.name,
		FetchedAt: time.Now().UTC(),
		Quotes:    []pricesource.PriceQuote{p.quote},
	}, nil
}

func TestRunTick_UsesFallbackProvider(t *testing.T) {
	t.Parallel()
	rec := &ingestRecorder{}
	primaryErr := pricesource.ProviderError{
		Provider:  "twelvedata",
		Retryable: false,
		Err:       errors.New("boom"),
	}
	p1 := &fakeProvider{name: "twelvedata", err: primaryErr}
	p2 := &fakeProvider{
		name: "backup",
		quote: pricesource.PriceQuote{
			Symbol:         "AAPL",
			Price:          decimal.RequireFromString("195.33"),
			Currency:       "USD",
			AsOf:           time.Unix(1710000000, 0).UTC(),
			SourceSequence: 100,
		},
	}
	partitions := config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 2)
	runner, err := New(rec, Config{
		Interval:              60 * time.Second,
		Symbols:               []string{"AAPL"},
		Providers:             []pricesource.PriceProvider{p1, p2},
		PriceStreamPartitions: partitions,
		MaxRetries:            0,
		RetryDelay:            10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := runner.runTick(t.Context()); err != nil {
		t.Fatalf("runTick: %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("events len got %d want 1", len(rec.events))
	}
	if got := rec.events[0].Source; got != "pricefeed:backup" {
		t.Fatalf("source got %q want pricefeed:backup", got)
	}
}

func TestEmitQuote_MonotonicSourceSequenceAndIdempotencyKey(t *testing.T) {
	t.Parallel()
	rec := &ingestRecorder{}
	partitions := config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 4)
	runner, err := New(rec, Config{
		Interval:              60 * time.Second,
		Symbols:               []string{"EUR-USD"},
		Providers:             []pricesource.PriceProvider{&fakeProvider{name: "twelvedata"}},
		PriceStreamPartitions: partitions,
		MaxRetries:            0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	asOf := time.Unix(1710000000, 0).UTC()
	q1 := pricesource.PriceQuote{
		Symbol:         "EUR/USD",
		Price:          decimal.RequireFromString("1.0812"),
		Currency:       "USD",
		AsOf:           asOf,
		SourceSequence: 10,
	}
	q2 := q1
	q2.SourceSequence = 9 // lower than prior, should be bumped

	if _, _, _, err := runner.emitQuote(t.Context(), "twelvedata", q1); err != nil {
		t.Fatalf("emitQuote q1: %v", err)
	}
	if _, _, _, err := runner.emitQuote(t.Context(), "twelvedata", q2); err != nil {
		t.Fatalf("emitQuote q2: %v", err)
	}
	if len(rec.events) != 2 {
		t.Fatalf("events len got %d want 2", len(rec.events))
	}

	var p1, p2 domain.PricePayload
	if err := json.Unmarshal(rec.events[0].Payload, &p1); err != nil {
		t.Fatalf("unmarshal p1: %v", err)
	}
	if err := json.Unmarshal(rec.events[1].Payload, &p2); err != nil {
		t.Fatalf("unmarshal p2: %v", err)
	}
	if p1.Symbol != "EUR-USD" || p2.Symbol != "EUR-USD" {
		t.Fatalf("normalized symbol mismatch: %q / %q", p1.Symbol, p2.Symbol)
	}
	if p2.SourceSequence <= p1.SourceSequence {
		t.Fatalf("source sequence not monotonic: p1=%d p2=%d", p1.SourceSequence, p2.SourceSequence)
	}
	wantIdem := "twelvedata:EUR-USD:1710000000"
	if rec.events[0].IdempotencyKey != wantIdem {
		t.Fatalf("idempotency key got %q want %q", rec.events[0].IdempotencyKey, wantIdem)
	}
}

func TestEmitQuote_DropsStaleQuotes(t *testing.T) {
	t.Parallel()
	rec := &ingestRecorder{}
	partitions := config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 2)
	runner, err := New(rec, Config{
		Interval:              60 * time.Second,
		Symbols:               []string{"AAPL"},
		Providers:             []pricesource.PriceProvider{&fakeProvider{name: "twelvedata"}},
		PriceStreamPartitions: partitions,
		MaxQuoteAge:           5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	q := pricesource.PriceQuote{
		Symbol:         "AAPL",
		Price:          decimal.RequireFromString("100"),
		Currency:       "USD",
		AsOf:           time.Now().UTC().Add(-10 * time.Minute),
		SourceSequence: 1,
	}
	ingested, droppedStale, dedupSkipped, err := runner.emitQuote(t.Context(), "twelvedata", q)
	if err != nil {
		t.Fatalf("emitQuote: %v", err)
	}
	if ingested || dedupSkipped {
		t.Fatalf("expected only stale drop, ingested=%v dedup=%v", ingested, dedupSkipped)
	}
	if !droppedStale {
		t.Fatalf("expected droppedStale=true")
	}
	if len(rec.events) != 0 {
		t.Fatalf("expected no events, got %d", len(rec.events))
	}
}

func TestEmitQuote_DedupSkipsUnchangedPrice(t *testing.T) {
	t.Parallel()
	rec := &ingestRecorder{}
	partitions := config.DerivePriceStreamPartitions(uuid.MustParse("00000000-0000-4000-8000-000000000001"), 2)
	runner, err := New(rec, Config{
		Interval:              60 * time.Second,
		Symbols:               []string{"AAPL"},
		Providers:             []pricesource.PriceProvider{&fakeProvider{name: "twelvedata"}},
		PriceStreamPartitions: partitions,
		DedupWindow:           time.Minute,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	base := time.Now().UTC()
	q1 := pricesource.PriceQuote{
		Symbol:         "AAPL",
		Price:          decimal.RequireFromString("200"),
		Currency:       "USD",
		AsOf:           base,
		SourceSequence: 1,
	}
	q2 := q1
	q2.AsOf = base.Add(10 * time.Second)

	if _, _, _, err := runner.emitQuote(t.Context(), "twelvedata", q1); err != nil {
		t.Fatalf("emitQuote q1: %v", err)
	}
	ingested, droppedStale, dedupSkipped, err := runner.emitQuote(t.Context(), "twelvedata", q2)
	if err != nil {
		t.Fatalf("emitQuote q2: %v", err)
	}
	if ingested || droppedStale {
		t.Fatalf("expected dedup skip only, ingested=%v stale=%v", ingested, droppedStale)
	}
	if !dedupSkipped {
		t.Fatalf("expected dedupSkipped=true")
	}
	if len(rec.events) != 1 {
		t.Fatalf("events len got %d want 1", len(rec.events))
	}
}
