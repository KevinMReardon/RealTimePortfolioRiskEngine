package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/api"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/events"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion/pricefeed"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/insights"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/proposals"
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

	var proposalStore *proposals.Store
	if cfg.ProposalsRuntimeEnabled() {
		proposalStore = proposals.NewStore(dbPool)
		logger.Info("proposals_store_wired",
			zap.Bool("proposal_store_ready", proposalStore != nil),
			zap.Bool("trading_halt_env", cfg.TradingHalt),
			zap.String("policy_mode", string(cfg.PolicyMode)),
		)
	}

	stopWorkers, err := startWorkers(workerCtx, repo, logger, cfg)
	if err != nil {
		return err
	}

	ingestSvc := ingestion.NewService(repo)

	alpacaSyncStore := alpaca.NewSyncStateStore(dbPool)

	bootCtx := context.Background()
	targets, err := repo.ListAlpacaSyncTargets(bootCtx)
	if err != nil {
		logger.Warn("alpaca_sync_targets_boot_load_failed", zap.Error(err))
	} else {
		for _, target := range targets {
			mode := strings.ToLower(strings.TrimSpace(target.AlpacaAccountMode))
			if mode == "" {
				mode = "paper"
			}
			rest, err := alpaca.NewREST(alpaca.RESTConfig{
				KeyID:     target.AlpacaKeyID,
				SecretKey: target.AlpacaSecretKey,
				BaseURL:   target.AlpacaBaseURL,
			})
			if err != nil {
				logger.Warn("alpaca_rest_init_failed",
					zap.String("portfolio_id", target.PortfolioID.String()),
					zap.String("mode", mode),
					zap.Error(err))
				continue
			}
			acct, err := rest.GetAccount(bootCtx)
			if err != nil {
				logger.Warn("alpaca_get_account_failed",
					zap.String("mode", mode),
					zap.String("portfolio_id", target.PortfolioID.String()),
					zap.Error(err))
				continue
			}
			if id := strings.TrimSpace(acct.ID); id != "" {
				if err := repo.SetPortfolioAlpacaAccountID(bootCtx, target.PortfolioID, id); err != nil {
					logger.Warn("alpaca_account_link_failed",
						zap.String("portfolio_id", target.PortfolioID.String()),
						zap.String("mode", mode),
						zap.Error(err))
				} else {
					logger.Info("alpaca_account_linked",
						zap.String("portfolio_id", target.PortfolioID.String()),
						zap.String("alpaca_account_mode", mode),
						zap.String("alpaca_account_id", id))
				}
			}
		}
	}

	var defaultAlpacaREST alpaca.REST
	if cfg.AlpacaImportJobsEnabled() {
		defaultMode := "paper"
		keyID := cfg.AlpacaPaperKeyID
		secret := cfg.AlpacaPaperSecretKey
		base := cfg.AlpacaPaperBaseURL
		if strings.TrimSpace(keyID) == "" || strings.TrimSpace(secret) == "" {
			defaultMode = "live"
			keyID = cfg.AlpacaLiveKeyID
			secret = cfg.AlpacaLiveSecretKey
			base = cfg.AlpacaLiveBaseURL
		}
		if strings.TrimSpace(keyID) != "" && strings.TrimSpace(secret) != "" {
			r, err := alpaca.NewREST(alpaca.RESTConfig{KeyID: keyID, SecretKey: secret, BaseURL: base})
			if err != nil {
				logger.Warn("alpaca_import_rest_init_failed", zap.String("mode", defaultMode), zap.Error(err))
			} else {
				defaultAlpacaREST = r
			}
		}
	}

	importCtx, importCancel := context.WithCancel(context.Background())
	defer importCancel()
	var importWG sync.WaitGroup
	if cfg.AlpacaImportJobsEnabled() && defaultAlpacaREST != nil {
		importWG.Add(1)
		go func() {
			defer importWG.Done()
			alpaca.RunImportJobWorker(importCtx, alpaca.ImportJobWorkerConfig{
				Jobs:       repo,
				Ingest:     ingestSvc,
				REST:       defaultAlpacaREST,
				Logger:     logger,
				PollEvery:  cfg.AlpacaImportJobPollInterval,
				JobTimeout: cfg.AlpacaImportJobTimeout,
			})
		}()
		logger.Info("alpaca_import_worker_spawned",
			zap.Duration("poll_every", cfg.AlpacaImportJobPollInterval),
			zap.Duration("job_timeout", cfg.AlpacaImportJobTimeout),
		)
	}

	alpacaCtx, alpacaCancel := context.WithCancel(context.Background())
	defer alpacaCancel()
	var alpacaWG sync.WaitGroup
	if cfg.AlpacaWorkerEnabled() {
		alpacaWG.Add(1)
		go func() {
			defer alpacaWG.Done()
			alpaca.RunSyncWorkers(alpacaCtx, alpaca.SyncWorkersConfig{
				Store:                 alpacaSyncStore,
				IngestSvc:             ingestSvc,
				Logger:                logger,
				Interval:              cfg.AlpacaSyncInterval,
				HistoryLookback:       cfg.AlpacaSyncHistoryLookback,
				PriceStreamPartitions: cfg.PriceStreamPartitions,
				ListTargets: func(ctx context.Context) ([]alpaca.SyncTarget, error) {
					rows, err := repo.ListAlpacaSyncTargets(ctx)
					if err != nil {
						return nil, err
					}
					out := make([]alpaca.SyncTarget, 0, len(rows))
					for _, row := range rows {
						out = append(out, alpaca.SyncTarget{
							PortfolioID:       row.PortfolioID,
							AlpacaAccountMode: row.AlpacaAccountMode,
							KeyID:             row.AlpacaKeyID,
							SecretKey:         row.AlpacaSecretKey,
							BaseURL:           row.AlpacaBaseURL,
						})
					}
					return out, nil
				},
			})
		}()
		logger.Info("alpaca_sync_worker_spawned",
			zap.Duration("interval", cfg.AlpacaSyncInterval),
			zap.Duration("history_lookback", cfg.AlpacaSyncHistoryLookback),
		)
	}

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

	var agentSvc agent.AgentService
	if cfg.AgentBriefingRuntimeEnabled() {
		if strings.TrimSpace(cfg.AnthropicAPIKey) == "" {
			logger.Warn("agent_briefing_disabled_missing_api_key")
		} else {
			anthropicClient := agent.NewHTTPAnthropicClient(cfg.AnthropicAPIKey, cfg.AnthropicBaseURL)
			toolExec := agent.NewToolDispatcher(repo, nil, nil)
			var proposalMaterializer agent.ProposalMaterializer
			if cfg.ProposalsRuntimeEnabled() && proposalStore != nil {
				proposalMaterializer = &agent.BriefingProposalMaterializer{
					Store:       proposalStore,
					Loader:      repo,
					Policy:      cfg.PolicyConfig(),
					TradingHalt: cfg.TradingHalt,
					Log:         logger,
				}
			}
			agentSvc = agent.NewServiceWithLoggerAndTimeout(
				repo,
				anthropicClient,
				toolExec,
				"anthropic",
				cfg.AgentModel,
				logger,
				cfg.AgentSessionTimeout,
				proposalMaterializer,
			)
			logger.Info("agent_briefing_enabled",
				zap.String("provider", "anthropic"),
				zap.String("model", cfg.AgentModel),
				zap.Bool("scheduler_enabled", cfg.AgentBriefingSchedulerRuntimeEnabled()),
				zap.Duration("session_timeout", cfg.AgentSessionTimeout),
			)
		}
	} else {
		logger.Info("agent_briefing_disabled", zap.String("reason", "config_disabled"))
	}

	agentBriefingCtx, agentBriefingCancel := context.WithCancel(context.Background())
	defer agentBriefingCancel()
	var agentBriefingWG sync.WaitGroup
	if agentSvc != nil && cfg.AgentBriefingSchedulerRuntimeEnabled() {
		sched, err := agent.NewBriefingCronScheduler(
			logger,
			agentSvc,
			repo,
			cfg.AgentBriefingCron,
			cfg.AgentBriefingTZ,
		)
		if err != nil {
			logger.Warn("agent_briefing_scheduler_init_failed", zap.Error(err))
		} else {
			agentBriefingWG.Add(1)
			go func() {
				defer agentBriefingWG.Done()
				sched.Run(agentBriefingCtx)
			}()
			logger.Info("agent_briefing_scheduler_spawned",
				zap.String("cron", cfg.AgentBriefingCron),
				zap.String("tz", cfg.AgentBriefingTZ),
			)
		}
	}

	alpacaImportAPIEnabled := cfg.AlpacaImportJobsEnabled() && defaultAlpacaREST != nil

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
		SingleUserApp:             cfg.SingleUserApp,
		PriceMarksRead:            repo,
		PriceFeedRuntime:          priceFeedRuntime,
		PriceFeedEnabled:          cfg.PriceFeedEnabled,
		PriceFeedProvider:         cfg.PriceFeedProvider,
		PriceFeedPollInterval:     cfg.PriceFeedPollInterval,
		PriceFeedWatchlistManager: priceFeedRunner,
		PriceFeedWatchlistStore:   repo,
		AlpacaImportJobs:          repo,
		AlpacaImportEnabled:       alpacaImportAPIEnabled,
		AlpacaREST:                defaultAlpacaREST,
		AlpacaSyncStore:           alpacaSyncStore,
		AlpacaConfigured:          defaultAlpacaREST != nil,
		AgentService:              agentSvc,
		AgentMaxTokens:            cfg.AgentMaxTokens,
		AgentTemperature:          cfg.AgentTemperature,
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
	importCancel()
	importStopped := make(chan struct{})
	go func() {
		defer close(importStopped)
		importWG.Wait()
	}()
	select {
	case <-importStopped:
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	}
	if cfg.AlpacaImportJobsEnabled() {
		logger.Info("alpaca_import_worker_stopped")
	}

	alpacaCancel()
	alpacaStopped := make(chan struct{})
	go func() {
		defer close(alpacaStopped)
		alpacaWG.Wait()
	}()
	select {
	case <-alpacaStopped:
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
	}
	if cfg.AlpacaWorkerEnabled() {
		logger.Info("alpaca_sync_stopped")
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

	agentBriefingCancel()
	agentBriefingStopped := make(chan struct{})
	go func() {
		defer close(agentBriefingStopped)
		agentBriefingWG.Wait()
	}()
	select {
	case <-agentBriefingStopped:
	case <-shutdownCtx.Done():
		return shutdownCtx.Err()
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
