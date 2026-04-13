// Package events implements the append-only event store, ordering semantics, and apply
// workers.
//
// Architecture (v1 in-process, intentional):
//   - Fixed-size worker pools — goroutine count is bounded by config (APPLY_WORKER_COUNT,
//     PRICE_APPLY_WORKER_COUNT), not by number of portfolios or price messages.
//   - Stable sharding — hash(portfolio_id) % P routes each partition to exactly one pool
//     goroutine so ordering is serialized per partition without O(N) goroutines.
//   - Discovery colocated in the pool loop — trade workers call ListPortfolioIDsNotIn on
//     each tick inside shardTick; there is no separate supervisor goroutine. PricePool
//     uses the fixed configured partition list (same tick model).
//   - Option B recovery — we persist the apply cursor in projection_cursor in the same DB
//     transaction as positions_projection / prices_projection updates (not Option A in-memory
//     only). See cursor.go and migrations/000003_projection_cursor.up.sql.
//
// Ordering key per partition is (event_time ASC, event_id ASC). Watermark: env
// ORDERING_WATERMARK_MS → W; eligible events satisfy event_time <= max_seen - W where
// max_seen is MaxEventTime for that partition (see trade_portfolio_apply.go pumpAfterCursor).
// Optional ORDERING_MAX_EVENT_AGE_MS DLQ skips events whose event_time is too far behind
// wall clock at apply time.
package events

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Worker runs a fixed pool of trade/portfolio apply goroutines (shardCount). Discovery of
// portfolio IDs is colocated in shardTick via ListPortfolioIDsNotIn each tick — no separate
// supervisor. Portfolios are assigned with workerShardFor so each portfolio_id is owned
// by exactly one goroutine (stable sharding).
//
// Apply-time no-shorting: on ErrPositionUnderflow the event is written to DLQ,
// the cursor is advanced past it, and processing continues. The violating trade
// is not applied to positions (projection stays consistent with enforceable history).
type Worker struct {
	repo                   Repository
	log                    *zap.Logger
	tick                   time.Duration
	watermark              time.Duration
	maxEventAge            time.Duration
	shardCount             int
	excludePricePartitions []uuid.UUID
	riskScheduler          RiskRecomputeScheduler
	snapshotMinEvents      int
	snapshotMinInterval    time.Duration
	shardWG                sync.WaitGroup
}

// NewWorker builds the trade apply pool. excludePricePartitions must list every synthetic
// price events.portfolio_id so trade workers never consume price partitions.
// maxEventAge 0 disables wall-clock stale DLQ; otherwise events with time.Since(event_time) > maxEventAge are DLQ'd.
func NewWorker(repo Repository, log *zap.Logger, tick, watermark, maxEventAge time.Duration, shardCount int, excludePricePartitions []uuid.UUID) *Worker {
	if tick <= 0 {
		tick = 500 * time.Millisecond
	}
	if shardCount < 1 {
		shardCount = 1
	}
	return &Worker{
		repo:                   repo,
		log:                    log,
		tick:                   tick,
		watermark:              watermark,
		maxEventAge:            maxEventAge,
		shardCount:             shardCount,
		excludePricePartitions: excludePricePartitions,
	}
}

// WithRiskScheduler installs a debounced risk trigger callback (optional).
func (w *Worker) WithRiskScheduler(s RiskRecomputeScheduler) *Worker {
	w.riskScheduler = s
	return w
}

// WithPortfolioSnapshotPolicy enables periodic `portfolio_snapshots` writes after trade
// ApplyBatch commits. A snapshot is taken when minEvents > 0 and applied envelopes since the
// last successful write reach minEvents, and/or when minInterval > 0 and minInterval has
// elapsed since the last successful write (wall clock). Both zero disables the feature.
// Configure via SNAPSHOT_EVERY_N_EVENTS, SNAPSHOT_MIN_INTERVAL_SEC, and SNAPSHOT_ENABLED (config).
func (w *Worker) WithPortfolioSnapshotPolicy(minEvents int, minInterval time.Duration) *Worker {
	if minEvents < 0 {
		minEvents = 0
	}
	if minInterval < 0 {
		minInterval = 0
	}
	w.snapshotMinEvents = minEvents
	w.snapshotMinInterval = minInterval
	return w
}

// Run starts shardCount goroutines until ctx is done.
func (w *Worker) Run(ctx context.Context) error {
	for shard := 0; shard < w.shardCount; shard++ {
		w.shardWG.Add(1)
		go func(shardID int) {
			defer w.shardWG.Done()
			w.runShard(ctx, shardID)
		}(shard)
	}
	<-ctx.Done()
	w.shardWG.Wait()
	return ctx.Err()
}

// workerShardFor is stable routing: the same pid always maps to the same shard index for a given shardCount.
func workerShardFor(pid uuid.UUID, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write(pid[:])
	return int(h.Sum32() % uint32(shardCount))
}

func (w *Worker) runShard(ctx context.Context, shard int) {
	portfolios := make(map[uuid.UUID]*partitionWorker)
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()

	w.shardTick(ctx, shard, portfolios)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.shardTick(ctx, shard, portfolios)
		}
	}
}

func (w *Worker) shardTick(ctx context.Context, shard int, portfolios map[uuid.UUID]*partitionWorker) {
	ids, err := w.repo.ListPortfolioIDsNotIn(ctx, w.excludePricePartitions)
	if err != nil {
		w.log.Warn("worker_list_portfolios_failed", zap.Int("shard", shard), zap.Error(err))
		return
	}
	for _, pid := range ids {
		if workerShardFor(pid, w.shardCount) != shard {
			continue
		}
		pw, ok := portfolios[pid]
		if !ok {
			pw = newPartitionWorker(w.repo, w.log, w.tick, w.watermark, w.maxEventAge, pid, w.snapshotMinEvents, w.snapshotMinInterval)
			pw.riskScheduler = w.riskScheduler
			portfolios[pid] = pw
			if err := pw.rebuild(ctx); err != nil {
				w.log.Error("partition_rebuild_failed", zap.String("portfolio_id", pw.pidStr), zap.Int("shard", shard), zap.Error(err))
				delete(portfolios, pid)
				continue
			}
			if err := pw.processIncremental(ctx); err != nil {
				w.log.Warn("partition_incremental_after_rebuild_failed", zap.String("portfolio_id", pw.pidStr), zap.Int("shard", shard), zap.Error(err))
			}
			continue
		}
		if err := pw.processIncremental(ctx); err != nil {
			w.log.Warn("partition_incremental_failed", zap.String("portfolio_id", pw.pidStr), zap.Int("shard", shard), zap.Error(err))
		}
	}
}
