package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/api"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion/pricefeed"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/insights"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/risk"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := observability.NewLogger()
	if err != nil {
		return err
	}
	defer func() {
		_ = logger.Sync()
	}()

	dbPool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer dbPool.Close()

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	repo := events.NewPostgresStore(dbPool)
	stopWorkers, err := startWorkers(workerCtx, repo, logger, cfg)
	if err != nil {
		return err
	}

	ingestSvc := ingestion.NewService(repo)
	var feedWG sync.WaitGroup
	feedCtx, feedCancel := context.WithCancel(context.Background())
	defer feedCancel()
	var priceFeedRuntime *pricefeed.RuntimeTracker
	var priceFeedRunner *pricefeed.PriceIngestor
	if cfg.PriceFeedEnabled {
		initialWatchlist := cfg.PriceFeedSymbols
		if persistedWatchlist, found, err := repo.LoadPriceFeedWatchlist(context.Background()); err != nil {
			return err
		} else if found {
			initialWatchlist = persistedWatchlist
			logger.Info("price_feed_watchlist_loaded",
				zap.Int("symbols", len(initialWatchlist)),
				zap.String("source", "database"),
			)
		}
		cfg.PriceFeedSymbols = initialWatchlist
		feedRunner, rt, err := pricefeed.NewFromConfig(ingestSvc, cfg, logger)
		if err != nil {
			return err
		}
		priceFeedRunner = feedRunner
		priceFeedRuntime = rt
		feedWG.Add(1)
		go func() {
			defer feedWG.Done()
			if err := feedRunner.Start(feedCtx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("price_feed_runner_exit", zap.Error(err))
			}
		}()
		logger.Info("price_feed_started",
			zap.Duration("interval", cfg.PriceFeedPollInterval),
			zap.Int("symbols", len(cfg.PriceFeedSymbols)),
			zap.Int("max_retries", cfg.PriceFeedMaxRetries),
			zap.Duration("retry_delay", cfg.PriceFeedRetryDelay),
			zap.Duration("max_quote_age", cfg.PriceFeedMaxQuoteAge),
			zap.Duration("dedup_window", cfg.PriceFeedDedupWindow),
			zap.Int("provider_rate_limit_rpm", cfg.PriceFeedTwelveDataRateLimitRPM),
		)
	}

	var ingestRL *api.PerIPRateLimiter
	if cfg.RateLimitIngestEnabled {
		ingestRL = api.NewPerIPRateLimiter(cfg.RateLimitIngestRPS, cfg.RateLimitIngestBurst)
	}
	var getRL *api.PerIPRateLimiter
	if cfg.RateLimitGetEnabled {
		getRL = api.NewPerIPRateLimiter(cfg.RateLimitGetRPS, cfg.RateLimitGetBurst)
	}
	if cfg.RateLimitIngestEnabled {
		logger.Info("http_rate_limit_ingest",
			zap.Int("rps", cfg.RateLimitIngestRPS),
			zap.Int("burst", cfg.RateLimitIngestBurst),
		)
	}
	if cfg.RateLimitGetEnabled {
		logger.Info("http_rate_limit_get",
			zap.Int("rps", cfg.RateLimitGetRPS),
			zap.Int("burst", cfg.RateLimitGetBurst),
		)
	}

	var insightsSvc api.InsightsService
	if cfg.OpenAIAPIKey != "" {
		insightsSvc = insights.NewOpenAIService(cfg.OpenAIAPIKey, cfg.OpenAIBaseURL, cfg.OpenAIModel)
		logger.Info("openai_insights_enabled",
			zap.String("model", cfg.OpenAIModel),
			zap.Bool("base_url_set", cfg.OpenAIBaseURL != ""),
		)
	}

	router := api.NewRouter(api.RouterConfig{
		Logger:                logger,
		Ingest:                ingestSvc,
		ReadPortfolio:         repo,
		PortfolioCatalog:      repo,
		RiskRead:              repo,
		RiskSigmaWindowN:      cfg.RiskSigmaWindowN,
		PriceStreamPartitions: cfg.PriceStreamPartitions,
		RateLimitIngest:       ingestRL,
		RateLimitGet:          getRL,
		Insights:              insightsSvc,
		PrometheusEnabled:     cfg.PrometheusEnabled,
		AuthStore:             repo,
		AuthConfig: api.AuthConfig{
			CookieSecure: cfg.AuthCookieSecure,
			SessionTTL:   cfg.AuthSessionTTL,
		},
		PriceMarksRead:            repo,
		PriceFeedRuntime:          priceFeedRuntime,
		PriceFeedEnabled:          cfg.PriceFeedEnabled,
		PriceFeedProvider:         cfg.PriceFeedProvider,
		PriceFeedPollInterval:     cfg.PriceFeedPollInterval,
		PriceFeedWatchlistManager: priceFeedRunner,
		PriceFeedWatchlistStore:   repo,
	})
	server := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("server_starting", zap.String("addr", server.Addr))
		serverErrCh <- server.ListenAndServe()
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case sig := <-signalCh:
		logger.Info("shutdown_signal_received", zap.String("signal", sig.String()))
	case err := <-serverErrCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := stopWorkers(shutdownCtx); err != nil {
		return err
	}
	feedCancel()
	feedStopped := make(chan struct{})
	go func() {
		defer close(feedStopped)
		feedWG.Wait()
	}()
	select {
	case <-feedStopped:
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	}
	if cfg.PriceFeedEnabled {
		logger.Info("price_feed_stopped")
	}

	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}

	logger.Info("server_stopped")
	return nil
}

// startWorkers starts two fixed pools: trade portfolios (stable shard of ListPortfolioIDsNotIn)
// and price partitions (stable shard of config-derived UUIDs). No separate supervisor;
// discovery runs inside each trade pool tick.
func startWorkers(ctx context.Context, repo *events.PostgresStore, logger *zap.Logger, cfg config.Config) (func(context.Context) error, error) {
	riskScheduler := events.NewDebouncedRiskScheduler(cfg.RiskRecomputeDebounce, func(portfolioID uuid.UUID) {
		recomputeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := recomputeAndMaybePersistRisk(recomputeCtx, repo, portfolioID, cfg); err != nil {
			logger.Warn("risk_recompute_failed", zap.String("portfolio_id", portfolioID.String()), zap.Error(err))
			return
		}
		logger.Debug("risk_recompute_ok", zap.String("portfolio_id", portfolioID.String()))
	})
	tradeW := events.NewWorker(repo, logger, cfg.ApplyWorkerTick, cfg.OrderingWatermark, cfg.OrderingMaxEventAge, cfg.ApplyWorkerCount, cfg.PriceStreamPartitions).
		WithRiskScheduler(riskScheduler).
		WithPortfolioSnapshotPolicy(cfg.PortfolioSnapshotMinEvents, cfg.PortfolioSnapshotInterval)
	priceW := events.NewPricePool(repo, logger, cfg.ApplyWorkerTick, cfg.OrderingWatermark, cfg.OrderingMaxEventAge, cfg.PriceApplyWorkerCount, cfg.PriceStreamPartitions).WithRiskScheduler(riskScheduler)

	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		if err := tradeW.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("trade_worker_exit", zap.Error(err))
		}
	}()
	go func() {
		if err := priceW.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("price_worker_exit", zap.Error(err))
		}
	}()

	logger.Info("workers_started",
		zap.Int("apply_worker_count", cfg.ApplyWorkerCount),
		zap.Int("price_apply_worker_count", cfg.PriceApplyWorkerCount),
		zap.Int("price_stream_shard_count", cfg.PriceStreamShardCount),
		zap.Duration("risk_recompute_debounce", cfg.RiskRecomputeDebounce),
		zap.Duration("apply_worker_tick", cfg.ApplyWorkerTick),
		zap.Duration("ordering_watermark", cfg.OrderingWatermark),
		zap.Duration("ordering_max_event_age", cfg.OrderingMaxEventAge),
		zap.Bool("snapshot_enabled", cfg.SnapshotEnabled),
		zap.Int("snapshot_every_n_events", cfg.PortfolioSnapshotMinEvents),
		zap.Duration("snapshot_min_interval", cfg.PortfolioSnapshotInterval),
	)
	if cfg.PrometheusEnabled {
		logger.Info("prometheus_metrics_enabled", zap.String("path", "/metrics"))
	}

	stopFn := func(shutdownCtx context.Context) error {
		_ = shutdownCtx
		cancel()
		riskScheduler.Stop()
		logger.Info("workers_stopped")
		return nil
	}

	return stopFn, nil
}

func recomputeAndMaybePersistRisk(ctx context.Context, repo *events.PostgresStore, portfolioID uuid.UUID, cfg config.Config) error {
	in, found, err := repo.LoadPortfolioAssemblerInput(ctx, portfolioID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}

	riskIn := risk.Input{
		Positions: make([]risk.PositionInput, 0, len(in.Positions)),
		Prices:    make(map[string]decimal.Decimal, len(in.PriceBySymbol)),
		Sigma1D:   make(map[string]decimal.Decimal),
	}
	openSymbols := make([]string, 0, len(in.Positions))
	for _, p := range in.Positions {
		if p.Quantity.IsZero() {
			continue
		}
		riskIn.Positions = append(riskIn.Positions, risk.PositionInput{
			Symbol:   p.Symbol,
			Quantity: p.Quantity,
		})
		openSymbols = append(openSymbols, p.Symbol)
		if mark, ok := in.PriceBySymbol[p.Symbol]; ok && !mark.Price.IsZero() {
			riskIn.Prices[p.Symbol] = mark.Price
		}
	}
	sigmas, err := repo.LoadSymbolSigma1D(ctx, openSymbols, cfg.RiskSigmaWindowN)
	if err != nil {
		return err
	}
	// Ensure every priced symbol has a sigma; sparse history defaults to 0 in v1.
	for sym := range riskIn.Prices {
		if s, ok := sigmas[sym]; ok {
			riskIn.Sigma1D[sym] = s
			continue
		}
		riskIn.Sigma1D[sym] = decimal.Zero
	}

	snap, err := risk.NewEngine().BuildSnapshot(riskIn)
	if err != nil {
		return err
	}
	if !cfg.RiskSnapshotWriteEnabled {
		return nil
	}
	var asOfID uuid.UUID
	var asOfTime time.Time
	if in.TradeApply != nil {
		asOfID = in.TradeApply.EventID
		asOfTime = in.TradeApply.EventTime
	}
	if asOfID == uuid.Nil || asOfTime.IsZero() {
		// v1 minimal lineage fallback: skip write when we do not have a clear as-of tuple.
		return nil
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return repo.UpsertRiskSnapshot(ctx, portfolioID, asOfTime, asOfID, body)
}
