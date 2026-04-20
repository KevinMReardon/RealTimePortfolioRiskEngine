package alpaca

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// SyncWorkerConfig wires Alpaca REST fill polling into canonical ingestion.
type SyncWorkerConfig struct {
	PortfolioID uuid.UUID

	REST      REST
	Store     *SyncStateStore
	IngestSvc ingestion.Service

	Logger *zap.Logger

	Interval        time.Duration
	HistoryLookback time.Duration

	PriceStreamPartitions []uuid.UUID
}

// SyncTarget identifies one portfolio and which Alpaca account mode to use.
type SyncTarget struct {
	PortfolioID       uuid.UUID
	AlpacaAccountMode string
	KeyID             string
	SecretKey         string
	BaseURL           string
}

// SyncWorkersConfig polls and syncs all linked Alpaca portfolios.
type SyncWorkersConfig struct {
	Store     *SyncStateStore
	IngestSvc ingestion.Service
	Logger    *zap.Logger

	Interval        time.Duration
	HistoryLookback time.Duration

	PriceStreamPartitions []uuid.UUID
	ListTargets           func(ctx context.Context) ([]SyncTarget, error)
}

// RunSyncWorker polls Alpaca activities until ctx is cancelled. Intended to run in a goroutine.
func RunSyncWorker(ctx context.Context, cfg SyncWorkerConfig) {
	if cfg.Logger == nil {
		return
	}
	if cfg.IngestSvc == nil || cfg.REST == nil || cfg.Store == nil {
		cfg.Logger.Error("alpaca_sync_worker_disabled", zap.String("reason", "nil_dependency"))
		return
	}
	if cfg.PortfolioID == uuid.Nil {
		cfg.Logger.Error("alpaca_sync_worker_disabled", zap.String("reason", "nil_portfolio"))
		return
	}
	if reservedPortfolioPartition(cfg.PortfolioID, cfg.PriceStreamPartitions) {
		cfg.Logger.Error("alpaca_sync_worker_disabled",
			zap.String("reason", "portfolio_is_reserved_price_shard"),
			zap.String("portfolio_id", cfg.PortfolioID.String()),
		)
		return
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 90 * time.Second
	}
	if cfg.HistoryLookback <= 0 {
		cfg.HistoryLookback = 365 * 24 * time.Hour
	}

	cfg.Logger.Info("alpaca_sync_worker_started",
		zap.String("portfolio_id", cfg.PortfolioID.String()),
		zap.Duration("interval", cfg.Interval),
		zap.Duration("history_lookback_default", cfg.HistoryLookback),
	)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	runOnce := func() {
		tickCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		err := syncFillsTick(tickCtx, cfg)
		if err != nil {
			cfg.Logger.Warn("alpaca_sync_tick_failed", zap.Error(err))
			observability.ObserveAlpacaSyncRun("error_during_tick")
			prev, prevErr := cfg.Store.Get(tickCtx, cfg.PortfolioID)
			if prevErr != nil {
				cfg.Logger.Warn("alpaca_sync_error_state_load_failed", zap.Error(prevErr))
				return
			}
			msg := err.Error()
			merged := SyncState{
				PortfolioID: cfg.PortfolioID,
				LastError:   &msg,
			}
			if prev != nil {
				merged.LastSuccessAt = cloneTimePtr(prev.LastSuccessAt)
				merged.ActivitiesAfterTime = cloneTimePtr(prev.ActivitiesAfterTime)
				merged.ActivitiesPageToken = cloneStrPtr(prev.ActivitiesPageToken)
			}
			if upErr := cfg.Store.Upsert(tickCtx, merged); upErr != nil {
				cfg.Logger.Warn("alpaca_sync_error_state_persist_failed", zap.Error(upErr))
			}
			return
		}
		observability.ObserveAlpacaSyncRun("ok")
	}

	runOnce()

	for {
		select {
		case <-ctx.Done():
			cfg.Logger.Info("alpaca_sync_worker_stopped")
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

// RunSyncWorkers polls linked portfolios and syncs each with the account mode mapped in RESTByMode.
func RunSyncWorkers(ctx context.Context, cfg SyncWorkersConfig) {
	if cfg.Logger == nil {
		return
	}
	if cfg.IngestSvc == nil || cfg.Store == nil || cfg.ListTargets == nil {
		cfg.Logger.Error("alpaca_sync_workers_disabled", zap.String("reason", "nil_dependency"))
		return
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 90 * time.Second
	}
	if cfg.HistoryLookback <= 0 {
		cfg.HistoryLookback = 365 * 24 * time.Hour
	}

	cfg.Logger.Info("alpaca_sync_workers_started",
		zap.Duration("interval", cfg.Interval),
		zap.Duration("history_lookback_default", cfg.HistoryLookback),
	)
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	restCache := map[string]REST{}

	runOnce := func() {
		tickCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		targets, err := cfg.ListTargets(tickCtx)
		if err != nil {
			cfg.Logger.Warn("alpaca_sync_targets_load_failed", zap.Error(err))
			observability.ObserveAlpacaSyncRun("error_during_tick")
			return
		}
		for _, target := range targets {
			mode := strings.ToLower(strings.TrimSpace(target.AlpacaAccountMode))
			if mode == "" {
				mode = "paper"
			}
			key := strings.TrimSpace(target.KeyID)
			secret := strings.TrimSpace(target.SecretKey)
			baseURL := strings.TrimSpace(target.BaseURL)
			if key == "" || secret == "" {
				cfg.Logger.Warn("alpaca_sync_target_skipped_missing_credentials",
					zap.String("portfolio_id", target.PortfolioID.String()),
					zap.String("alpaca_account_mode", mode),
				)
				continue
			}
			cacheKey := mode + "|" + key + "|" + baseURL
			rest := restCache[cacheKey]
			if rest == nil {
				var err error
				rest, err = NewREST(RESTConfig{
					KeyID:     key,
					SecretKey: secret,
					BaseURL:   baseURL,
				})
				if err != nil {
					cfg.Logger.Warn("alpaca_sync_target_skipped_invalid_credentials",
						zap.String("portfolio_id", target.PortfolioID.String()),
						zap.String("alpaca_account_mode", mode),
						zap.Error(err),
					)
					continue
				}
				restCache[cacheKey] = rest
			}
			wcfg := SyncWorkerConfig{
				PortfolioID:           target.PortfolioID,
				REST:                  rest,
				Store:                 cfg.Store,
				IngestSvc:             cfg.IngestSvc,
				Logger:                cfg.Logger,
				Interval:              cfg.Interval,
				HistoryLookback:       cfg.HistoryLookback,
				PriceStreamPartitions: cfg.PriceStreamPartitions,
			}
			if err := syncFillsTick(tickCtx, wcfg); err != nil {
				cfg.Logger.Warn("alpaca_sync_tick_failed",
					zap.String("portfolio_id", target.PortfolioID.String()),
					zap.String("alpaca_account_mode", mode),
					zap.Error(err),
				)
				observability.ObserveAlpacaSyncRun("error_during_tick")
				prev, prevErr := cfg.Store.Get(tickCtx, target.PortfolioID)
				if prevErr != nil {
					cfg.Logger.Warn("alpaca_sync_error_state_load_failed", zap.Error(prevErr))
					continue
				}
				msg := err.Error()
				merged := SyncState{PortfolioID: target.PortfolioID, LastError: &msg}
				if prev != nil {
					merged.LastSuccessAt = cloneTimePtr(prev.LastSuccessAt)
					merged.ActivitiesAfterTime = cloneTimePtr(prev.ActivitiesAfterTime)
					merged.ActivitiesPageToken = cloneStrPtr(prev.ActivitiesPageToken)
				}
				if upErr := cfg.Store.Upsert(tickCtx, merged); upErr != nil {
					cfg.Logger.Warn("alpaca_sync_error_state_persist_failed", zap.Error(upErr))
				}
				continue
			}
			observability.ObserveAlpacaSyncRun("ok")
		}
	}

	runOnce()
	for {
		select {
		case <-ctx.Done():
			cfg.Logger.Info("alpaca_sync_workers_stopped")
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

func reservedPortfolioPartition(portfolioID uuid.UUID, shards []uuid.UUID) bool {
	for _, id := range shards {
		if portfolioID == id {
			return true
		}
	}
	return false
}

func syncFillsTick(ctx context.Context, cfg SyncWorkerConfig) error {
	baseState, err := cfg.Store.Get(ctx, cfg.PortfolioID)
	if err != nil {
		return fmt.Errorf("sync state load: %w", err)
	}

	var cursor SyncState
	cursor.PortfolioID = cfg.PortfolioID
	if baseState != nil {
		cursor.LastSuccessAt = cloneTimePtr(baseState.LastSuccessAt)
		cursor.ActivitiesAfterTime = cloneTimePtr(baseState.ActivitiesAfterTime)
		cursor.ActivitiesPageToken = cloneStrPtr(baseState.ActivitiesPageToken)
	}

	var tickMax time.Time
	var tickMaxSet bool

	for {
		req := ListActivitiesRequest{
			ActivityTypes: []string{FillActivityType},
			Direction:     "asc",
			PageSize:      100,
		}
		if cursor.ActivitiesPageToken != nil && strings.TrimSpace(*cursor.ActivitiesPageToken) != "" {
			req.PageToken = strings.TrimSpace(*cursor.ActivitiesPageToken)
		} else if cursor.ActivitiesAfterTime != nil {
			req.After = *cursor.ActivitiesAfterTime
		} else {
			req.After = time.Now().UTC().Add(-cfg.HistoryLookback)
		}

		page, err := cfg.REST.ListActivities(ctx, req)
		if err != nil {
			return fmt.Errorf("list activities: %w", err)
		}

		for _, act := range page.Activities {
			if !strings.EqualFold(act.ActivityType, FillActivityType) {
				continue
			}
			ts := act.TransactionTime.UTC()
			if ts.After(tickMax) || !tickMaxSet {
				tickMax = ts
				tickMaxSet = true
			}

			out, err := TryIngestFillActivity(ctx, cfg.IngestSvc, cfg.PortfolioID, act)
			if err != nil {
				cfg.Logger.Warn("alpaca_fill_ingest_failed",
					zap.String("activity_id", act.ID),
					zap.String("symbol", act.Symbol),
					zap.Error(err),
				)
				observability.ObserveAlpacaFillOutcome("skipped_invalid")
				continue
			}
			switch out {
			case OutcomeSkippedInvalid:
				cfg.Logger.Debug("alpaca_fill_skipped",
					zap.String("activity_id", act.ID),
				)
				observability.ObserveAlpacaFillOutcome("skipped_invalid")
			case OutcomeAppended:
				cfg.Logger.Info("alpaca_fill_ingested",
					zap.String("portfolio_id", cfg.PortfolioID.String()),
					zap.String("activity_id", act.ID),
				)
				observability.ObserveAlpacaFillOutcome("appended")
			case OutcomeDuplicate:
				observability.ObserveAlpacaFillOutcome("duplicate")
			}
		}

		nextTok := strings.TrimSpace(page.NextPageToken)

		if nextTok != "" {
			token := nextTok
			up := SyncState{
				PortfolioID:         cfg.PortfolioID,
				LastSuccessAt:       nil,
				LastError:           nil,
				ActivitiesPageToken: &token,
				ActivitiesAfterTime: cursor.ActivitiesAfterTime,
			}
			if err := cfg.Store.Upsert(ctx, up); err != nil {
				return fmt.Errorf("persist sync cursor (page): %w", err)
			}
			cursor.ActivitiesPageToken = &token
			continue
		}

		now := time.Now().UTC()
		finalAfter := cursor.ActivitiesAfterTime
		if tickMaxSet {
			tMax := tickMax.UTC()
			finalAfter = &tMax
		}

		up := SyncState{
			PortfolioID:         cfg.PortfolioID,
			LastSuccessAt:       &now,
			LastError:           nil,
			ActivitiesPageToken: nil,
			ActivitiesAfterTime: finalAfter,
		}
		if err := cfg.Store.Upsert(ctx, up); err != nil {
			return fmt.Errorf("persist sync cursor (final): %w", err)
		}
		break
	}

	return nil
}

func cloneTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	tt := t.UTC()
	return &tt
}

func cloneStrPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}
