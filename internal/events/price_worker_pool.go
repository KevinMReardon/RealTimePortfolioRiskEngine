package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
)

// PricePool runs a fixed pool of goroutines (workerCount) over configured synthetic price
// partitions. Partition list is config-derived, not DB-discovered. workerShardFor assigns
// each partition UUID to one goroutine (same stable sharding model as Worker). Each
// symbol routes to one partition at ingest; each partition has its own projection_cursor row.
type PricePool struct {
	repo          Repository
	log           *zap.Logger
	tick          time.Duration
	watermark     time.Duration
	maxEventAge   time.Duration
	workerCount   int
	partitions    []uuid.UUID
	riskScheduler RiskRecomputeScheduler
	shardWG       sync.WaitGroup
}

// NewPricePool builds the price apply pool. workerCount is often set higher than trade ApplyWorkerCount.
func NewPricePool(repo Repository, log *zap.Logger, tick, watermark, maxEventAge time.Duration, workerCount int, partitions []uuid.UUID) *PricePool {
	if tick <= 0 {
		tick = 500 * time.Millisecond
	}
	if workerCount < 1 {
		workerCount = 1
	}
	if len(partitions) == 0 {
		panic("events: NewPricePool requires at least one price partition")
	}
	return &PricePool{
		repo:        repo,
		log:         log,
		tick:        tick,
		watermark:   watermark,
		maxEventAge: maxEventAge,
		workerCount: workerCount,
		partitions:  partitions,
	}
}

// WithRiskScheduler installs a debounced risk trigger callback (optional).
func (p *PricePool) WithRiskScheduler(s RiskRecomputeScheduler) *PricePool {
	p.riskScheduler = s
	return p
}

// Run starts workerCount goroutines until ctx is done.
func (p *PricePool) Run(ctx context.Context) error {
	for shard := 0; shard < p.workerCount; shard++ {
		p.shardWG.Add(1)
		go func(shardID int) {
			defer p.shardWG.Done()
			p.runShard(ctx, shardID)
		}(shard)
	}
	<-ctx.Done()
	p.shardWG.Wait()
	return ctx.Err()
}

func (p *PricePool) runShard(ctx context.Context, shard int) {
	partitions := make(map[uuid.UUID]*pricePartitionWorker)
	ticker := time.NewTicker(p.tick)
	defer ticker.Stop()

	p.shardTick(ctx, shard, partitions)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.shardTick(ctx, shard, partitions)
		}
	}
}

func (p *PricePool) shardTick(ctx context.Context, shard int, partitions map[uuid.UUID]*pricePartitionWorker) {
	for _, pid := range p.partitions {
		if workerShardFor(pid, p.workerCount) != shard {
			continue
		}
		pw, ok := partitions[pid]
		if !ok {
			pw = newPricePartitionWorker(p.repo, p.log, p.tick, p.watermark, p.maxEventAge, pid)
			pw.riskScheduler = p.riskScheduler
			partitions[pid] = pw
			if err := pw.rebuild(ctx); err != nil {
				p.log.Error("price_partition_rebuild_failed", zap.String("partition_id", pw.pidStr), zap.Int("shard", shard), zap.Error(err))
				delete(partitions, pid)
				continue
			}
			if err := pw.pumpAfterCursor(ctx); err != nil {
				p.log.Warn("price_partition_incremental_after_rebuild_failed", zap.String("partition_id", pw.pidStr), zap.Int("shard", shard), zap.Error(err))
			}
			continue
		}
		if err := pw.pumpAfterCursor(ctx); err != nil {
			p.log.Warn("price_partition_incremental_failed", zap.String("partition_id", pw.pidStr), zap.Int("shard", shard), zap.Error(err))
		}
	}
}

type pricePartitionWorker struct {
	repo          Repository
	log           *zap.Logger
	tick          time.Duration
	watermark     time.Duration
	maxEventAge   time.Duration
	pid           uuid.UUID
	pidStr        string
	cur           Cursor
	riskScheduler RiskRecomputeScheduler
}

func newPricePartitionWorker(repo Repository, log *zap.Logger, tick, watermark, maxEventAge time.Duration, pid uuid.UUID) *pricePartitionWorker {
	return &pricePartitionWorker{
		repo:        repo,
		log:         log,
		tick:        tick,
		watermark:   watermark,
		maxEventAge: maxEventAge,
		pid:         pid,
		pidStr:      pid.String(),
	}
}

// rebuild: Option B for this price shard — non-zero cursor means DB projections already
// match applied events; tail is pumped via FetchAfter (no in-memory cache of all symbols).
func (w *pricePartitionWorker) rebuild(ctx context.Context) error {
	cur, err := w.repo.LoadProjectionCursor(ctx, w.pid)
	if err != nil {
		return err
	}
	w.cur = Cursor{}
	if cur.IsZero() {
		evs, err := w.repo.FetchAllForPortfolio(ctx, w.pid)
		if err != nil {
			return err
		}
		return w.applyPriceEligible(ctx, evs)
	}
	w.cur = cur
	return w.pumpAfterCursor(ctx)
}

// pumpAfterCursor matches trade path: cutoff = max_seen - W (ORDERING_WATERMARK_MS), then
// eligible prefix and ApplyPriceBatch (prices_projection + cursor in one transaction).
func (w *pricePartitionWorker) pumpAfterCursor(ctx context.Context) error {
	for {
		maxT, err := w.repo.MaxEventTime(ctx, w.pid)
		if err != nil {
			return err
		}
		cutoff := maxT.Add(-w.watermark)
		batch, err := w.repo.FetchAfter(ctx, w.pid, w.cur, fetchAfterPageLimit)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		eligible := eligibleBatchAfterWatermark(batch, cutoff, w.watermark, fetchAfterPageLimit)
		if len(eligible) == 0 {
			return nil
		}
		if err := w.applyPriceEligible(ctx, eligible); err != nil {
			return err
		}
		if len(eligible) < len(batch) || len(batch) < fetchAfterPageLimit {
			return nil
		}
	}
}

func (w *pricePartitionWorker) applyPriceEligible(ctx context.Context, eligible []domain.EventEnvelope) error {
	var okBatch []domain.EventEnvelope
	batchSymbols := make(map[string]struct{})
	for _, ev := range eligible {
		if eventTooStaleForApply(ev, w.maxEventAge) {
			if len(okBatch) > 0 {
				if err := w.repo.ApplyPriceBatch(ctx, w.pid, okBatch); err != nil {
					return err
				}
				if err := w.scheduleRiskForSymbols(ctx, batchSymbols); err != nil {
					w.log.Warn("risk_schedule_after_price_batch_failed", zap.String("partition_id", w.pidStr), zap.Error(err))
				}
				last := okBatch[len(okBatch)-1]
				w.cur = CursorFromEvent(last)
				observability.ObserveProjectionLag("price", last.EventTime)
				okBatch = nil
				batchSymbols = make(map[string]struct{})
			}
			staleErr := fmt.Errorf("event_time too far behind apply time (max_age=%s)", w.maxEventAge)
			if dqErr := w.repo.PersistApplyDLQ(ctx, w.pid, w.pidStr, ev, "EVENT_TOO_STALE", staleErr); dqErr != nil {
				w.log.Error("price_worker_dlq_failed", zap.Error(dqErr))
				return dqErr
			}
			w.log.Warn("price_worker_event_too_stale_dlq", zap.String("partition_id", w.pidStr), zap.String("event_id", ev.EventID.String()))
			w.cur = CursorFromEvent(ev)
			observability.ObserveProjectionLag("price", ev.EventTime)
			continue
		}
		if ev.EventType != domain.EventTypePriceUpdated {
			continue
		}
		var p domain.PricePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			if len(okBatch) > 0 {
				if err := w.repo.ApplyPriceBatch(ctx, w.pid, okBatch); err != nil {
					return err
				}
				if err := w.scheduleRiskForSymbols(ctx, batchSymbols); err != nil {
					w.log.Warn("risk_schedule_after_price_batch_failed", zap.String("partition_id", w.pidStr), zap.Error(err))
				}
				last := okBatch[len(okBatch)-1]
				w.cur = CursorFromEvent(last)
				observability.ObserveProjectionLag("price", last.EventTime)
				okBatch = nil
				batchSymbols = make(map[string]struct{})
			}
			if dqErr := w.repo.PersistApplyDLQ(ctx, w.pid, w.pidStr, ev, "BAD_PRICE_PAYLOAD", err); dqErr != nil {
				w.log.Error("price_worker_dlq_failed", zap.Error(dqErr))
				return dqErr
			}
			w.cur = CursorFromEvent(ev)
			continue
		}
		batchSymbols[p.Symbol] = struct{}{}
		okBatch = append(okBatch, ev)
	}
	if len(okBatch) > 0 {
		if err := w.repo.ApplyPriceBatch(ctx, w.pid, okBatch); err != nil {
			return err
		}
		if err := w.scheduleRiskForSymbols(ctx, batchSymbols); err != nil {
			w.log.Warn("risk_schedule_after_price_batch_failed", zap.String("partition_id", w.pidStr), zap.Error(err))
		}
		last := okBatch[len(okBatch)-1]
		w.cur = CursorFromEvent(last)
		observability.ObserveProjectionLag("price", last.EventTime)
	}
	return nil
}

func (w *pricePartitionWorker) scheduleRiskForSymbols(ctx context.Context, symbols map[string]struct{}) error {
	if w.riskScheduler == nil || len(symbols) == 0 {
		return nil
	}
	keys := make([]string, 0, len(symbols))
	for s := range symbols {
		keys = append(keys, s)
	}
	pids, err := w.repo.ListPortfolioIDsByOpenSymbols(ctx, keys)
	if err != nil {
		return err
	}
	for _, pid := range pids {
		w.riskScheduler.Schedule(pid)
	}
	return nil
}
