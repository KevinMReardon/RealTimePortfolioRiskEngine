package events

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/portfolio"
)

// partitionWorker holds apply state for one customer portfolio. Exactly one pool goroutine
// drives it (Worker.shardTick); that goroutine may own many partitionWorkers sequentially.
type partitionWorker struct {
	repo          Repository
	log           *zap.Logger
	tick          time.Duration
	watermark     time.Duration
	maxEventAge   time.Duration
	pid           uuid.UUID
	pidStr        string
	agg           *portfolio.Aggregate
	cur           Cursor
	riskScheduler RiskRecomputeScheduler

	snapshotMinEvents          int
	snapshotMinInterval        time.Duration
	eventsAppliedSinceSnapshot int
	lastSnapshotAt             time.Time // wall clock after last successful InsertPortfolioSnapshot
}

func newPartitionWorker(repo Repository, log *zap.Logger, tick, watermark, maxEventAge time.Duration, pid uuid.UUID, snapshotMinEvents int, snapshotMinInterval time.Duration) *partitionWorker {
	return &partitionWorker{
		repo:                repo,
		log:                 log,
		tick:                tick,
		watermark:           watermark,
		maxEventAge:         maxEventAge,
		pid:                 pid,
		pidStr:              pid.String(),
		agg:                 portfolio.NewAggregate(),
		snapshotMinEvents:   snapshotMinEvents,
		snapshotMinInterval: snapshotMinInterval,
		lastSnapshotAt:      time.Now(),
	}
}

func (pw *partitionWorker) snapshotPolicyEnabled() bool {
	return pw.snapshotMinEvents > 0 || pw.snapshotMinInterval > 0
}

func (pw *partitionWorker) shouldWritePortfolioSnapshot() bool {
	byN := pw.snapshotMinEvents > 0 && pw.eventsAppliedSinceSnapshot >= pw.snapshotMinEvents
	byT := pw.snapshotMinInterval > 0 && time.Since(pw.lastSnapshotAt) >= pw.snapshotMinInterval
	return byN || byT
}

// maybeSnapshotAfterApply runs after ApplyBatch + cursor advance; uses last envelope in
// applied as (as_of_event_time, as_of_event_id), matching projection_cursor.
// Snapshots are best-effort: marshal/insert/cursor-alignment failures log and return; the
// event counter keeps growing so a later batch retries. Writes only when reloaded
// projection_cursor equals the batch tail so as_of is never ahead of (or divergent from) DB truth.
func (pw *partitionWorker) maybeSnapshotAfterApply(ctx context.Context, applied []domain.EventEnvelope) {
	if !pw.snapshotPolicyEnabled() || len(applied) == 0 {
		return
	}
	pw.eventsAppliedSinceSnapshot += len(applied)
	if !pw.shouldWritePortfolioSnapshot() {
		return
	}
	last := applied[len(applied)-1]
	body, err := portfolio.MarshalTradePositionsSnapshotV1(pw.agg, pw.pidStr)
	if err != nil {
		pw.log.Warn("portfolio_snapshot_marshal_failed", zap.String("portfolio_id", pw.pidStr), zap.Error(err))
		return
	}
	dbCur, err := pw.repo.LoadProjectionCursor(ctx, pw.pid)
	if err != nil {
		pw.log.Warn("portfolio_snapshot_cursor_reload_failed",
			zap.String("portfolio_id", pw.pidStr),
			zap.Error(err),
		)
		return
	}
	lastCur := CursorFromEvent(last)
	if CompareCursors(lastCur, dbCur) != 0 {
		pw.log.Warn("portfolio_snapshot_skipped_not_aligned_with_projection_cursor",
			zap.String("portfolio_id", pw.pidStr),
			zap.Time("batch_last_event_time", lastCur.Time.UTC()),
			zap.String("batch_last_event_id", lastCur.ID.String()),
			zap.Time("projection_cursor_time", dbCur.Time.UTC()),
			zap.String("projection_cursor_id", dbCur.ID.String()),
		)
		return
	}
	res, err := pw.repo.InsertPortfolioSnapshot(ctx, pw.pid, last.EventTime, last.EventID, body)
	if err != nil {
		pw.log.Warn("portfolio_snapshot_insert_failed", zap.String("portfolio_id", pw.pidStr), zap.Error(err))
		return
	}
	pw.eventsAppliedSinceSnapshot = 0
	pw.lastSnapshotAt = time.Now()
	if res.Inserted {
		pw.log.Debug("portfolio_snapshot_written",
			zap.String("portfolio_id", pw.pidStr),
			zap.Time("as_of_event_time", last.EventTime.UTC()),
			zap.String("as_of_event_id", last.EventID.String()),
		)
	}
}

func (pw *partitionWorker) flushApplyBatch(ctx context.Context, okBatch []domain.EventEnvelope) error {
	if len(okBatch) == 0 {
		return nil
	}
	if err := pw.repo.ApplyBatch(ctx, pw.pid, pw.pidStr, okBatch, pw.agg); err != nil {
		return err
	}
	if pw.riskScheduler != nil {
		pw.riskScheduler.Schedule(pw.pid)
	}
	last := okBatch[len(okBatch)-1]
	pw.cur = CursorFromEvent(last)
	observability.ObserveProjectionLag("trade", last.EventTime)
	pw.maybeSnapshotAfterApply(ctx, okBatch)
	return nil
}

// eligiblePrefix returns the longest prefix of batch with event_time <= cutoff (apply order preserved).
func eligiblePrefix(batch []domain.EventEnvelope, cutoff time.Time) []domain.EventEnvelope {
	n := 0
	for n < len(batch) && !batch[n].EventTime.After(cutoff) {
		n++
	}
	return batch[:n]
}

const fetchAfterPageLimit = 200

// eligibleBatchAfterWatermark applies the ordering buffer (event_time <= max_seen - W).
// If that prefix is empty but the batch is a **partial** FetchAfter page (len < limit), returns
// the full batch. Otherwise the last event(s) at max_seen can never satisfy the cutoff (they are
// always within W of max_seen), which deadlocks single-event tails—common for price shards that
// got an empty rebuild before the first PriceUpdated arrived.
func eligibleBatchAfterWatermark(batch []domain.EventEnvelope, cutoff time.Time, watermark time.Duration, fetchLimit int) []domain.EventEnvelope {
	eligible := eligiblePrefix(batch, cutoff)
	if len(eligible) == 0 && len(batch) > 0 && len(batch) < fetchLimit && watermark > 0 {
		return batch
	}
	return eligible
}

func eventTooStaleForApply(ev domain.EventEnvelope, maxAge time.Duration) bool {
	if maxAge <= 0 {
		return false
	}
	return time.Since(ev.EventTime) > maxAge
}

// rebuild: try latest portfolio_snapshots first when aligned with projection_cursor; else
// Option B — cursor set → hydrate positions_projection + tail; cursor zero → full replay.
//
// Restart invariant: restart = latest snapshot + replay tail of events; equivalent to never stopping,
// modulo ordering policy (LLD section 5.1 portfolio_snapshots, section 6 ordering; pumpAfterCursor in this file).
func (pw *partitionWorker) rebuild(ctx context.Context) error {
	dbCur, err := pw.repo.LoadProjectionCursor(ctx, pw.pid)
	if err != nil {
		return err
	}

	snap, snapErr := pw.repo.LoadLatestPortfolioSnapshot(ctx, pw.pid)
	if snapErr != nil {
		pw.log.Warn("portfolio_snapshot_load_failed",
			zap.String("portfolio_id", pw.pidStr),
			zap.Error(snapErr),
		)
		snap = LatestPortfolioSnapshot{Found: false}
	}

	pw.agg = portfolio.NewAggregate()
	pw.cur = Cursor{}

	useSnapshot := snap.Found && snap.Aggregate != nil && !snap.AsOf.IsZero()
	if useSnapshot {
		if !dbCur.IsZero() {
			cmp := CompareCursors(snap.AsOf, dbCur)
			if cmp < 0 {
				pw.log.Debug("portfolio_snapshot_stale_ignored",
					zap.String("portfolio_id", pw.pidStr),
				)
				useSnapshot = false
			} else if cmp > 0 {
				pw.log.Warn("portfolio_snapshot_ahead_of_cursor_ignored",
					zap.String("portfolio_id", pw.pidStr),
				)
				useSnapshot = false
			}
		}
	}

	if useSnapshot {
		pw.agg = snap.Aggregate
		pw.cur = snap.AsOf
		pw.log.Debug("portfolio_rebuild_from_snapshot",
			zap.String("portfolio_id", pw.pidStr),
			zap.Time("as_of_event_time", snap.AsOf.Time.UTC()),
			zap.String("as_of_event_id", snap.AsOf.ID.String()),
		)
		return pw.pumpAfterCursor(ctx)
	}

	if dbCur.IsZero() {
		evs, err := pw.repo.FetchAllForPortfolio(ctx, pw.pid)
		if err != nil {
			return err
		}
		return pw.applyEligible(ctx, evs)
	}

	pw.cur = dbCur
	if err := pw.repo.LoadPortfolioPositionsIntoAggregate(ctx, pw.pid, pw.pidStr, pw.agg); err != nil {
		return err
	}
	return pw.pumpAfterCursor(ctx)
}

func (pw *partitionWorker) processIncremental(ctx context.Context) error {
	return pw.pumpAfterCursor(ctx)
}

// pumpAfterCursor: cutoff = max_seen - W with max_seen from MaxEventTime and W from
// ORDERING_WATERMARK_MS (0 disables). FetchAfter returns the next page; eligible prefix is
// event_time <= cutoff; ApplyBatch persists positions + cursor in one transaction.
func (pw *partitionWorker) pumpAfterCursor(ctx context.Context) error {
	for {
		maxT, err := pw.repo.MaxEventTime(ctx, pw.pid)
		if err != nil {
			return err
		}
		cutoff := maxT.Add(-pw.watermark)
		batch, err := pw.repo.FetchAfter(ctx, pw.pid, pw.cur, fetchAfterPageLimit)
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		eligible := eligibleBatchAfterWatermark(batch, cutoff, pw.watermark, fetchAfterPageLimit)
		if len(eligible) == 0 {
			return nil
		}
		if err := pw.applyEligible(ctx, eligible); err != nil {
			return err
		}
		if len(eligible) < len(batch) || len(batch) < fetchAfterPageLimit {
			return nil
		}
	}
}

// applyEligible applies events to the aggregate and commits with ApplyBatch / DLQ. Flushes a batch before DLQ on underflow.
func (pw *partitionWorker) applyEligible(ctx context.Context, eligible []domain.EventEnvelope) error {
	var okBatch []domain.EventEnvelope
	for _, ev := range eligible {
		if eventTooStaleForApply(ev, pw.maxEventAge) {
			if len(okBatch) > 0 {
				if err := pw.flushApplyBatch(ctx, okBatch); err != nil {
					return err
				}
				okBatch = nil
			}
			staleErr := fmt.Errorf("event_time too far behind apply time (max_age=%s)", pw.maxEventAge)
			if dqErr := pw.repo.PersistApplyDLQ(ctx, pw.pid, pw.pidStr, ev, "EVENT_TOO_STALE", staleErr); dqErr != nil {
				pw.log.Error("worker_dlq_failed", zap.String("portfolio_id", pw.pidStr), zap.Error(dqErr))
				return dqErr
			}
			pw.log.Warn("worker_event_too_stale_dlq",
				zap.String("portfolio_id", pw.pidStr),
				zap.String("event_id", ev.EventID.String()),
			)
			pw.cur = CursorFromEvent(ev)
			observability.ObserveProjectionLag("trade", ev.EventTime)
			continue
		}
		err := pw.agg.ApplyEvent(pw.pidStr, ev)
		if err == nil {
			okBatch = append(okBatch, ev)
			continue
		}
		if len(okBatch) > 0 {
			if err := pw.flushApplyBatch(ctx, okBatch); err != nil {
				return err
			}
			okBatch = nil
		}
		if errors.Is(err, domain.ErrPositionUnderflow) {
			if dqErr := pw.repo.PersistApplyDLQ(ctx, pw.pid, pw.pidStr, ev, "POSITION_UNDERFLOW", err); dqErr != nil {
				pw.log.Error("worker_dlq_failed", zap.String("portfolio_id", pw.pidStr), zap.Error(dqErr))
				return dqErr
			}
			pw.log.Warn("worker_position_underflow_dlq",
				zap.String("portfolio_id", pw.pidStr),
				zap.String("event_id", ev.EventID.String()),
			)
			pw.cur = CursorFromEvent(ev)
			observability.ObserveProjectionLag("trade", ev.EventTime)
			continue
		}
		return err
	}
	if len(okBatch) > 0 {
		if err := pw.flushApplyBatch(ctx, okBatch); err != nil {
			return err
		}
	}
	return nil
}
