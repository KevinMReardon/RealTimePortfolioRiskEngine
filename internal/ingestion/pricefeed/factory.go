package pricefeed

import (
	"fmt"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/config"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/ingestion"
	"github.com/KevinMReardon/realtime-portfolio-risk/internal/pricesource"
	"go.uber.org/zap"
)

// NewFromConfig builds a PriceIngestor for PRICE_FEED_PROVIDER (twelvedata or alpaca).
// The returned RuntimeTracker is updated by the ingestor each poll tick (for status APIs).
func NewFromConfig(svc ingestion.Service, cfg config.Config, logger *zap.Logger) (*PriceIngestor, *RuntimeTracker, error) {
	provider, err := priceFeedProviderFromConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

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

func priceFeedProviderFromConfig(cfg config.Config) (pricesource.PriceProvider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.PriceFeedProvider)) {
	case "alpaca":
		keyID := strings.TrimSpace(cfg.AlpacaPaperKeyID)
		secret := strings.TrimSpace(cfg.AlpacaPaperSecretKey)
		if keyID == "" || secret == "" {
			keyID = strings.TrimSpace(cfg.AlpacaLiveKeyID)
			secret = strings.TrimSpace(cfg.AlpacaLiveSecretKey)
		}
		if keyID == "" || secret == "" {
			return nil, fmt.Errorf("PRICE_FEED_PROVIDER=alpaca requires ALPACA_PAPER_* or ALPACA_LIVE_* credentials")
		}
		base := cfg.AlpacaDataBaseURL
		if strings.TrimSpace(base) == "" {
			base = pricesource.DefaultAlpacaMarketDataURL
		}
		return pricesource.NewAlpacaMarketDataProvider(
			keyID,
			secret,
			base,
			cfg.PriceFeedHTTPTimeout,
			cfg.PriceFeedAlpacaRateLimitRPM,
		), nil
	default:
		return pricesource.NewTwelveDataProvider(
			cfg.PriceFeedTwelveDataAPIKey,
			cfg.PriceFeedHTTPTimeout,
			cfg.PriceFeedTwelveDataRateLimitRPM,
		), nil
	}
}
