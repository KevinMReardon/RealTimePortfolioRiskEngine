package api

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/connectors/alpaca"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion/pricefeed"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/observability"
)

type healthResponse struct {
	Status string `json:"status"`
}

// RouterConfig holds HTTP dependencies. Ingest may be nil to skip trade routes.
type RouterConfig struct {
	Logger        *zap.Logger
	Ingest        ingestion.Service
	ReadPortfolio PortfolioReadStore
	// PortfolioCatalog enables GET/POST /v1/portfolios directory operations.
	PortfolioCatalog PortfolioCatalogStore
	// RiskRead loads sigma/return stats; nil skips GET /v1/portfolios/:id/risk.
	RiskRead RiskReadStore
	// RiskSigmaWindowN is passed to return-window queries (e.g. 60).
	RiskSigmaWindowN int
	// PriceStreamPartitions must match worker/config (synthetic events.portfolio_id shards for prices).
	PriceStreamPartitions []uuid.UUID
	// RateLimitIngest / RateLimitGet are optional per-IP token buckets; nil disables.
	RateLimitIngest *PerIPRateLimiter
	RateLimitGet    *PerIPRateLimiter
	// Insights is optional AI explain (OpenAI). Nil when OPENAI_API_KEY is unset — route still
	// registers and returns HTTP 503 + INSUFFICIENT_DATA (details.reason OPENAI_NOT_CONFIGURED).
	Insights InsightsService
	// PrometheusEnabled gates GET /metrics exposure.
	PrometheusEnabled bool
	// AuthStore enables backend account/session APIs and protected routes when set.
	AuthStore AuthStore
	// AuthConfig controls session cookie behavior.
	AuthConfig AuthConfig
	// SingleUserApp rejects creating a second catalog portfolio when true (matches config.SINGLE_USER_APP).
	SingleUserApp bool

	// PriceMarksRead enables GET /v1/prices and GET /v1/prices/:symbol.
	PriceMarksRead PriceMarksReader
	// PriceFeedRuntime is optional in-process feed telemetry (nil when PRICE_FEED_ENABLED=false).
	PriceFeedRuntime *pricefeed.RuntimeTracker
	PriceFeedEnabled bool
	// PriceFeedProvider is the configured adapter name: twelvedata or alpaca.
	PriceFeedProvider string
	// PriceFeedPollInterval is used for staleness thresholds in price list rows.
	PriceFeedPollInterval time.Duration
	// PriceFeedWatchlistManager exposes in-process watchlist read/write controls.
	PriceFeedWatchlistManager PriceFeedWatchlistManager
	// PriceFeedWatchlistStore persists watchlist changes across restarts.
	PriceFeedWatchlistStore PriceFeedWatchlistPersistence

	// AlpacaImportJobs enables POST enqueue + GET status when non-nil and AlpacaImportEnabled.
	AlpacaImportJobs    AlpacaImportStore
	AlpacaImportEnabled bool
	// AlpacaREST is the trading API client; nil when keys are not configured.
	AlpacaREST alpaca.REST
	// AlpacaSyncStore reads per-portfolio rows in alpaca_sync_state; may be non-nil when pool exists.
	AlpacaSyncStore *alpaca.SyncStateStore
	// AlpacaConfigured is true when the process successfully constructed an Alpaca REST client.
	AlpacaConfigured bool
}

// NewRouter builds the API router and wires baseline middleware/handlers.
func NewRouter(cfg RouterConfig) *gin.Engine {
	logger := cfg.Logger
	router := gin.New()

	router.Use(RequestIDMiddleware())
	router.Use(gin.Recovery())
	router.Use(requestLoggingMiddleware(logger))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, healthResponse{Status: "ok"})
	})
	if cfg.PrometheusEnabled {
		router.GET("/metrics", gin.WrapH(observability.MetricsHandler()))
	}

	if cfg.Ingest != nil && len(cfg.PriceStreamPartitions) > 0 {
		ing := router.Group("/v1")
		ing.Use(PerIPRateLimitMiddleware(cfg.RateLimitIngest))
		if cfg.AuthStore != nil {
			ing.Use(requireAuth(cfg.AuthStore))
		}
		ing.POST("/trades", postTradeHandler(cfg.Ingest, logger, cfg.PriceStreamPartitions, cfg.PortfolioCatalog))
		ing.POST("/prices", postPriceHandler(cfg.Ingest, logger, cfg.PriceStreamPartitions))
	}
	if cfg.ReadPortfolio != nil && len(cfg.PriceStreamPartitions) > 0 {
		read := router.Group("/v1")
		read.Use(PerIPRateLimitMiddleware(cfg.RateLimitGet))
		if cfg.AuthStore != nil {
			read.Use(requireAuth(cfg.AuthStore))
		}
		if cfg.PortfolioCatalog != nil {
			read.GET("/portfolios", listPortfoliosHandler(cfg.PortfolioCatalog, logger, cfg.PriceStreamPartitions))
			read.POST("/portfolios", createPortfolioHandler(cfg.PortfolioCatalog, logger, cfg.PriceStreamPartitions, cfg.SingleUserApp))
		}
		read.GET("/portfolios/:id", getPortfolioHandler(cfg.ReadPortfolio, logger, cfg.PriceStreamPartitions))
		read.GET("/portfolios/:id/alpaca/status", getAlpacaPortfolioStatusHandler(
			cfg.ReadPortfolio,
			cfg.AlpacaSyncStore,
			cfg.AlpacaREST,
			cfg.AlpacaConfigured,
			logger,
			cfg.PriceStreamPartitions,
		))
		read.GET("/portfolios/:id/alpaca/reconciliation", getAlpacaPortfolioReconciliationHandler(
			cfg.ReadPortfolio,
			cfg.AlpacaREST,
			cfg.AlpacaConfigured,
			logger,
			cfg.PriceStreamPartitions,
		))
		if cfg.RiskRead != nil {
			read.GET("/portfolios/:id/risk", getRiskHandler(cfg.RiskRead, logger, cfg.PriceStreamPartitions, cfg.RiskSigmaWindowN))
		}
		read.POST("/portfolios/:id/scenarios", postScenarioHandler(cfg.ReadPortfolio, logger, cfg.PriceStreamPartitions))
		read.POST("/portfolios/:id/insights/explain", postInsightsExplainHandler(cfg.ReadPortfolio, cfg.RiskRead, cfg.Insights, logger, cfg.PriceStreamPartitions, cfg.RiskSigmaWindowN))
		if cfg.PriceMarksRead != nil {
			staleAfter := 3 * cfg.PriceFeedPollInterval
			if staleAfter <= 0 {
				staleAfter = 15 * time.Minute
			}
			read.GET("/prices", listPricesHandler(cfg.PriceMarksRead, logger, staleAfter))
			read.GET("/prices/:symbol", getPriceSymbolHandler(cfg.PriceMarksRead, logger, staleAfter))
			read.GET("/price-feed/status", getPriceFeedStatusHandler(
				cfg.PriceFeedRuntime,
				cfg.PriceFeedEnabled,
				cfg.PriceFeedProvider,
				cfg.PriceFeedPollInterval,
				cfg.PriceFeedWatchlistManager,
			))
			read.GET("/price-feed/watchlist", getPriceFeedWatchlistHandler(cfg.PriceFeedWatchlistManager))
			read.PUT("/price-feed/watchlist", putPriceFeedWatchlistHandlerWithPersistence(
				cfg.PriceFeedWatchlistManager,
				cfg.PriceFeedWatchlistStore,
			))
		}
		if cfg.AlpacaImportJobs != nil {
			read.POST("/portfolios/:id/alpaca-import", postAlpacaImportHandler(
				cfg.AlpacaImportJobs,
				logger,
				cfg.PriceStreamPartitions,
				portfolioOwnershipChecker(cfg.ReadPortfolio),
				cfg.AlpacaImportEnabled,
			))
			read.GET("/alpaca-import-jobs/:job_id", getAlpacaImportJobHandler(
				cfg.AlpacaImportJobs,
				logger,
				portfolioOwnershipChecker(cfg.ReadPortfolio),
				cfg.AlpacaImportEnabled,
			))
		}
	}
	if cfg.AuthStore != nil {
		auth := router.Group("/v1/auth")
		auth.Use(PerIPRateLimitMiddleware(cfg.RateLimitIngest))
		if !cfg.SingleUserApp {
			auth.POST("/register", registerHandler(cfg.AuthStore, cfg.AuthConfig))
		}
		auth.POST("/login", loginHandler(cfg.AuthStore, cfg.AuthConfig))
		auth.POST("/logout", requireAuth(cfg.AuthStore), logoutHandler(cfg.AuthStore, cfg.AuthConfig))
		auth.GET("/me", requireAuth(cfg.AuthStore), meHandler())
	}

	return router
}

func portfolioOwnershipChecker(r PortfolioReadStore) PortfolioOwnershipChecker {
	if r == nil {
		return nil
	}
	c, ok := r.(PortfolioOwnershipChecker)
	if !ok {
		return nil
	}
	return c
}

func requestLoggingMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		logger.Info(
			"http_request",
			zap.String("request_id", RequestIDFromContext(c)),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Int64("latency_ms", time.Since(start).Milliseconds()),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}
