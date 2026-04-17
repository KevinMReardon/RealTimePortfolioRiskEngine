package pricefeed

import (
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/pricesource"
	"go.uber.org/zap"
)

// NewFromConfig builds a PriceIngestor configured for the current Twelve Data setup.
// The returned RuntimeTracker is updated by the ingestor each poll tick (for status APIs).
func NewFromConfig(svc ingestion.Service, cfg config.Config, logger *zap.Logger) (*PriceIngestor, *RuntimeTracker, error) {
	provider := pricesource.NewTwelveDataProvider(
		cfg.PriceFeedTwelveDataAPIKey,
		cfg.PriceFeedHTTPTimeout,
		cfg.PriceFeedTwelveDataRateLimitRPM,
	)
	rt := NewRuntimeTracker()
	ing, err := New(svc, Config{
		Interval:              cfg.PriceFeedPollInterval,
		Symbols:               cfg.PriceFeedSymbols,
		Providers:             []pricesource.PriceProvider{provider},
		PriceStreamPartitions: cfg.PriceStreamPartitions,
		MaxRetries:            cfg.PriceFeedMaxRetries,
		RetryDelay:            cfg.PriceFeedRetryDelay,
		SourcePrefix:          "pricefeed",
		MaxQuoteAge:           cfg.PriceFeedMaxQuoteAge,
		DedupWindow:           cfg.PriceFeedDedupWindow,
		Logger:                logger,
		Runtime:               rt,
	})
	if err != nil {
		return nil, nil, err
	}
	return ing, rt, nil
}
