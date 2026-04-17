package pricefeed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/pricesource"
	"go.uber.org/zap"
)

type Config struct {
	Interval              time.Duration
	Symbols               []string
	Providers             []pricesource.PriceProvider
	PriceStreamPartitions []uuid.UUID
	MaxRetries            int
	RetryDelay            time.Duration
	SourcePrefix          string
	// MaxQuoteAge rejects quotes whose as-of time is older than this (0 disables).
	MaxQuoteAge time.Duration
	// DedupWindow skips unchanged prices within this window (0 disables).
	DedupWindow time.Duration
	Logger      *zap.Logger
	// Runtime, when set, records tick outcomes for GET /v1/price-feed/status.
	Runtime *RuntimeTracker
}

type dedupState struct {
	price    decimal.Decimal
	lastSeen time.Time
}

type PriceIngestor struct {
	cfg Config
	svc ingestion.Service

	symbolsMu sync.RWMutex
	symbols   []string

	seqMu   sync.Mutex
	lastSeq map[string]int64
	dedupMu sync.Mutex
	dedup   map[string]dedupState
	rng     *rand.Rand
	log     *zap.Logger
}

func New(svc ingestion.Service, cfg Config) (*PriceIngestor, error) {
	if svc == nil {
		return nil, fmt.Errorf("ingestion service is required")
	}
	if len(cfg.Providers) == 0 {
		return nil, fmt.Errorf("at least one provider is required")
	}
	if len(cfg.PriceStreamPartitions) == 0 {
		return nil, fmt.Errorf("price stream partitions are required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 60 * time.Second
	}
	if cfg.RetryDelay <= 0 {
		cfg.RetryDelay = 500 * time.Millisecond
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.SourcePrefix == "" {
		cfg.SourcePrefix = "pricefeed"
	}
	return &PriceIngestor{
		cfg:     cfg,
		symbols: append([]string(nil), cfg.Symbols...),
		svc:     svc,
		lastSeq: make(map[string]int64, len(cfg.Symbols)),
		dedup:   make(map[string]dedupState, len(cfg.Symbols)),
		rng:     rand.New(rand.NewSource(time.Now().UnixNano())),
		log:     cfg.Logger,
	}, nil
}

// Start runs a long-lived polling loop until ctx is cancelled.
func (r *PriceIngestor) Start(ctx context.Context) error {
	if err := r.runTick(ctx); err != nil && !errors.Is(err, context.Canceled) {
		// Keep loop alive even if a single tick fails.
	}
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.runTick(ctx); err != nil && errors.Is(err, context.Canceled) {
				return err
			}
		}
	}
}

func (r *PriceIngestor) runTick(ctx context.Context) error {
	tickStart := time.Now().UTC()
	if r.cfg.Runtime != nil {
		r.cfg.Runtime.OnTickStart(tickStart)
	}
	res, providerName, usedFailover, err := r.fetchWithFailover(ctx)
	if err != nil {
		if r.cfg.Runtime != nil {
			r.cfg.Runtime.OnTickFailure(time.Now().UTC(), err)
		}
		if r.log != nil {
			r.log.Warn("price_feed_fetch_failed", zap.Error(err))
		}
		return err
	}
	ingestedCount := 0
	droppedStaleCount := 0
	dedupSkippedCount := 0
	for _, q := range res.Quotes {
		ingested, droppedStale, dedupSkipped, err := r.emitQuote(ctx, providerName, q)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			if r.log != nil {
				r.log.Warn("price_feed_emit_failed",
					zap.String("provider", providerName),
					zap.String("symbol", q.Symbol),
					zap.Error(err),
				)
			}
			// Continue processing remaining quotes.
			continue
		}
		if ingested {
			ingestedCount++
		}
		if droppedStale {
			droppedStaleCount++
		}
		if dedupSkipped {
			dedupSkippedCount++
		}
	}
	if ingestedCount > 0 {
		observability.AddPriceFeedSymbolsIngested(providerName, ingestedCount)
	}
	if droppedStaleCount > 0 {
		observability.AddPriceFeedDroppedStaleQuotes(providerName, droppedStaleCount)
		if r.log != nil {
			r.log.Info("price_feed_dropped_stale_quotes",
				zap.String("provider", providerName),
				zap.Int("count", droppedStaleCount),
			)
		}
	}
	if dedupSkippedCount > 0 {
		observability.AddPriceFeedDedupSkipped(providerName, dedupSkippedCount)
		if r.log != nil {
			r.log.Info("price_feed_dedup_skipped",
				zap.String("provider", providerName),
				zap.Int("count", dedupSkippedCount),
			)
		}
	}
	if r.cfg.Runtime != nil {
		r.cfg.Runtime.OnTickSuccess(time.Now().UTC(), providerName, usedFailover, ingestedCount)
	}
	return nil
}

func (r *PriceIngestor) Watchlist() []string {
	if r == nil {
		return nil
	}
	r.symbolsMu.RLock()
	defer r.symbolsMu.RUnlock()
	return append([]string(nil), r.symbols...)
}

func (r *PriceIngestor) SetWatchlist(symbols []string) {
	if r == nil {
		return
	}
	normalized := normalizeSymbols(symbols)
	r.symbolsMu.Lock()
	r.symbols = normalized
	r.symbolsMu.Unlock()
}

func (r *PriceIngestor) currentSymbols() []string {
	r.symbolsMu.RLock()
	defer r.symbolsMu.RUnlock()
	return append([]string(nil), r.symbols...)
}

func (r *PriceIngestor) fetchWithFailover(ctx context.Context) (pricesource.FetchResult, string, bool, error) {
	var lastErr error
	var previousProvider string
	for i, provider := range r.cfg.Providers {
		start := time.Now()
		res, err := r.fetchFromProviderWithRetry(ctx, provider)
		if err == nil {
			observability.ObservePriceFeedFetch(provider.Name(), time.Since(start), len(res.Quotes))
			if i > 0 {
				observability.IncPriceFeedProviderFailover(previousProvider, provider.Name())
				if r.log != nil {
					r.log.Warn("price_feed_provider_failover",
						zap.String("from_provider", previousProvider),
						zap.String("to_provider", provider.Name()),
					)
				}
			}
			if r.log != nil {
				r.log.Info("price_feed_fetch_ok",
					zap.String("provider", provider.Name()),
					zap.Duration("latency", time.Since(start)),
					zap.Int("symbols_fetched", len(res.Quotes)),
					zap.Bool("partial", res.Partial),
				)
			}
			return res, provider.Name(), i > 0, nil
		}
		if r.log != nil {
			r.log.Warn("price_feed_provider_failed",
				zap.String("provider", provider.Name()),
				zap.Error(err),
			)
		}
		lastErr = err
		previousProvider = provider.Name()
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no providers configured")
	}
	return pricesource.FetchResult{}, "", false, lastErr
}

func (r *PriceIngestor) fetchFromProviderWithRetry(ctx context.Context, provider pricesource.PriceProvider) (pricesource.FetchResult, error) {
	attempts := r.cfg.MaxRetries + 1
	var lastErr error
	symbols := r.currentSymbols()
	for attempt := 0; attempt < attempts; attempt++ {
		res, err := provider.FetchQuotes(ctx, symbols)
		if err == nil {
			return res, nil
		}
		var providerErr pricesource.ProviderError
		if errors.As(err, &providerErr) && providerErr.StatusCode == 429 {
			observability.IncPriceFeedRateLimitHit(provider.Name())
		}
		lastErr = err
		if !isRetryableFetchError(err) || attempt == attempts-1 {
			break
		}
		if err := r.sleepBackoff(ctx, attempt); err != nil {
			return pricesource.FetchResult{}, err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("fetch failed")
	}
	return pricesource.FetchResult{}, fmt.Errorf("%s fetch failed: %w", provider.Name(), lastErr)
}

func (r *PriceIngestor) sleepBackoff(ctx context.Context, attempt int) error {
	base := r.cfg.RetryDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	// Exponential backoff capped at 16x base.
	multiplier := 1 << attempt
	if multiplier > 16 {
		multiplier = 16
	}
	delay := time.Duration(multiplier) * base
	jitter := time.Duration(r.rng.Int63n(int64(250 * time.Millisecond)))
	wait := delay + jitter
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *PriceIngestor) emitQuote(ctx context.Context, providerName string, q pricesource.PriceQuote) (bool, bool, bool, error) {
	symbol, err := pricesource.NormalizeToInternalSymbol(q.Symbol)
	if err != nil {
		return false, false, false, err
	}
	partition, err := config.PricePartitionForSymbol(r.cfg.PriceStreamPartitions, symbol)
	if err != nil {
		return false, false, false, err
	}
	asOf := q.AsOf.UTC()
	if asOf.IsZero() {
		asOf = time.Now().UTC()
	}
	if r.cfg.MaxQuoteAge > 0 && time.Since(asOf) > r.cfg.MaxQuoteAge {
		return false, true, false, nil
	}
	if r.shouldDedup(symbol, q.Price, asOf) {
		return false, false, true, nil
	}
	seq := r.nextMonotonicSequence(symbol, q.SourceSequence)
	payload := domain.PricePayload{
		Symbol:         symbol,
		Price:          q.Price,
		Currency:       q.Currency,
		SourceSequence: seq,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return false, false, false, err
	}
	source := fmt.Sprintf("%s:%s", r.cfg.SourcePrefix, providerName)
	event := domain.EventEnvelope{
		EventID:        uuid.New(),
		EventType:      domain.EventTypePriceUpdated,
		EventTime:      asOf,
		ProcessingTime: time.Now().UTC(),
		Source:         source,
		PortfolioID:    partition.String(),
		IdempotencyKey: r.idempotencyKey(providerName, symbol, asOf),
		Payload:        payloadJSON,
	}
	_, err = r.svc.Ingest(ctx, event)
	if err != nil {
		return false, false, false, err
	}
	r.recordDedup(symbol, q.Price, asOf)
	return true, false, false, nil
}

func (r *PriceIngestor) shouldDedup(symbol string, price decimal.Decimal, asOf time.Time) bool {
	if r.cfg.DedupWindow <= 0 {
		return false
	}
	r.dedupMu.Lock()
	defer r.dedupMu.Unlock()
	prev, ok := r.dedup[symbol]
	if !ok {
		return false
	}
	if asOf.Sub(prev.lastSeen) > r.cfg.DedupWindow {
		return false
	}
	return prev.price.Equal(price)
}

func (r *PriceIngestor) recordDedup(symbol string, price decimal.Decimal, asOf time.Time) {
	if r.cfg.DedupWindow <= 0 {
		return
	}
	r.dedupMu.Lock()
	defer r.dedupMu.Unlock()
	r.dedup[symbol] = dedupState{price: price, lastSeen: asOf}
}

func (r *PriceIngestor) nextMonotonicSequence(symbol string, candidate int64) int64 {
	r.seqMu.Lock()
	defer r.seqMu.Unlock()
	last := r.lastSeq[symbol]
	if candidate <= 0 {
		candidate = time.Now().UTC().UnixMilli()
	}
	if candidate <= last {
		candidate = last + 1
	}
	r.lastSeq[symbol] = candidate
	return candidate
}

func (r *PriceIngestor) idempotencyKey(providerName, symbol string, ts time.Time) string {
	bucket := ts.UTC().Truncate(r.cfg.Interval).Unix()
	return fmt.Sprintf("%s:%s:%d", providerName, symbol, bucket)
}

func isRetryableFetchError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr pricesource.ProviderError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable
	}
	return false
}

func normalizeSymbols(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		v := strings.ToUpper(strings.TrimSpace(s))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
