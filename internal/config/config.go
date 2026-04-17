package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultPort                  = "8080"
	defaultShutdownTimeoutSecond = 10
	// defaultOrderingWatermarkMS matches LLD §16 (ORDERING_WATERMARK_MS).
	defaultOrderingWatermarkMS = 2000
	// defaultApplyWorkerTickMS: poll interval per shard; LLD does not name this key; 500ms matches prior NewWorker default.
	defaultApplyWorkerTickMS       = 500
	defaultApplyWorkerCount        = 8
	defaultPriceApplyWorkers       = 16
	defaultPriceStreamShards       = 16
	defaultRiskRecomputeDebounceMS = 250
	defaultRiskSigmaWindowN        = 60
	// defaultPriceStreamPortfolioID namespaces derived price partition UUIDs (not used as a row key itself when shards>0).
	defaultPriceStreamPortfolioID = "00000000-0000-4000-8000-000000000001"
	defaultPriceFeedProvider      = "twelvedata"
	defaultPriceFeedPollSeconds   = 60
	defaultPriceFeedHTTPTimeoutMS = 5000
	defaultPriceFeedRetryCount    = 3
	defaultPriceFeedRetryDelayMS  = 500
	defaultPriceFeedMaxQuoteAgeMS = 30 * 60 * 1000 // 30m
	defaultPriceFeedDedupWindowMS = 60 * 1000      // 60s
	defaultAuthSessionTTLSec      = 14 * 24 * 60 * 60
)

type Config struct {
	Port            string
	DatabaseURL     string
	ShutdownTimeout time.Duration
	// OrderingWatermark is W in the rule: only apply events with event_time <= max_seen - W,
	// where max_seen is the latest event_time in that partition (see events.MaxEventTime).
	// Env: ORDERING_WATERMARK_MS (LLD default 2000 ms). Use 0 to disable the buffer.
	OrderingWatermark time.Duration
	// OrderingMaxEventAge is optional wall-clock guard at apply time: if time.Since(event_time) exceeds
	// this, the event is DLQ'd and the cursor advances. 0 disables. Env: ORDERING_MAX_EVENT_AGE_MS.
	OrderingMaxEventAge time.Duration
	// ApplyWorkerTick is how often each shard polls ListPortfolioIDs and applies work.
	// Env: APPLY_WORKER_TICK_MS (default 500).
	ApplyWorkerTick time.Duration
	// ApplyWorkerCount is how many goroutines run the apply loop. Each portfolio is
	// assigned to exactly one worker (hash of portfolio_id); each worker handles many portfolios.
	ApplyWorkerCount int
	// PriceStreamNamespace UUID used with DerivePriceStreamPartitions. Env: PRICE_STREAM_PORTFOLIO_ID.
	PriceStreamNamespace uuid.UUID
	// PriceStreamPartitions are synthetic events.portfolio_id values (one per shard) for PriceUpdated only.
	PriceStreamPartitions []uuid.UUID
	// PriceStreamShardCount is len(PriceStreamPartitions) after load. Env: PRICE_STREAM_SHARD_COUNT.
	PriceStreamShardCount int
	// PriceApplyWorkerCount is goroutines applying price partitions (often > trade workers). Env: PRICE_APPLY_WORKER_COUNT.
	PriceApplyWorkerCount int
	// RiskRecomputeDebounce coalesces triggers per portfolio after trade/price applies.
	// Env: RISK_RECOMPUTE_DEBOUNCE_MS, clamped to [100,500] per LLD §8.1.
	RiskRecomputeDebounce time.Duration
	// RiskSigmaWindowN is the rolling return window used to estimate per-symbol sigma_1d.
	// Env: RISK_SIGMA_WINDOW_N (default 60).
	RiskSigmaWindowN int
	// RiskSnapshotWriteEnabled toggles optional persistence into risk_snapshots.
	// Env: RISK_SNAPSHOT_WRITE_ENABLED.
	RiskSnapshotWriteEnabled bool
	// SnapshotEnabled turns portfolio snapshot writes on or off (e.g. tests set false).
	// Env: SNAPSHOT_ENABLED (default true; when false, N and interval are ignored).
	SnapshotEnabled bool
	// PortfolioSnapshotMinEvents triggers portfolio_snapshots after this many applied
	// envelopes (per batch sum) since the last successful write. 0 disables the count trigger.
	// Env: SNAPSHOT_EVERY_N_EVENTS, legacy PORTFOLIO_SNAPSHOT_MIN_EVENTS.
	PortfolioSnapshotMinEvents int
	// PortfolioSnapshotInterval is the minimum wall-clock time between snapshot writes
	// for a partition (from last successful insert). 0 disables the time trigger.
	// Env: SNAPSHOT_MIN_INTERVAL_SEC, legacy PORTFOLIO_SNAPSHOT_INTERVAL_SEC.
	PortfolioSnapshotInterval time.Duration

	// HTTP per-IP rate limits (token bucket). Disabled by default for backward compatibility.
	// Env: HTTP_RATE_LIMIT_INGEST_ENABLED, HTTP_RATE_LIMIT_INGEST_RPS, HTTP_RATE_LIMIT_INGEST_BURST.
	RateLimitIngestEnabled bool
	RateLimitIngestRPS     int
	RateLimitIngestBurst   int
	// Env: HTTP_RATE_LIMIT_GET_ENABLED, HTTP_RATE_LIMIT_GET_RPS, HTTP_RATE_LIMIT_GET_BURST (optional GET /v1/portfolios/:id).
	RateLimitGetEnabled bool
	RateLimitGetRPS     int
	RateLimitGetBurst   int

	// OpenAIAPIKey from OPENAI_API_KEY (trimmed). Empty disables AI insights: cmd/server wires a nil
	// api.InsightsService; POST /v1/portfolios/:id/insights/explain returns HTTP 503 with error_code
	// INSUFFICIENT_DATA and details.reason OPENAI_NOT_CONFIGURED (distinct from missing projection data).
	OpenAIAPIKey string
	// OpenAIBaseURL from OPENAI_BASE_URL (default https://api.openai.com/v1 when key is set and base empty).
	OpenAIBaseURL string
	// OpenAIModel from OPENAI_MODEL (default gpt-4o-mini when key is set and model empty).
	OpenAIModel string
	// PrometheusEnabled toggles GET /metrics exposure.
	// Env: PROMETHEUS_ENABLED.
	PrometheusEnabled bool

	// PriceFeedEnabled toggles automated provider polling.
	// Env: PRICE_FEED_ENABLED.
	PriceFeedEnabled bool
	// PriceFeedProvider selects the active provider adapter.
	// Env: PRICE_FEED_PROVIDER (currently only "twelvedata" is supported).
	PriceFeedProvider string
	// PriceFeedPollInterval is provider fetch cadence.
	// Env: PRICE_FEED_POLL_SECONDS.
	PriceFeedPollInterval time.Duration
	// PriceFeedSymbols is the configured watchlist.
	// Env: PRICE_FEED_SYMBOLS (comma-separated).
	PriceFeedSymbols []string
	// PriceFeedHTTPTimeout is request timeout per provider call.
	// Env: PRICE_FEED_HTTP_TIMEOUT_MS.
	PriceFeedHTTPTimeout time.Duration
	// PriceFeedMaxRetries is retry attempts for transient failures.
	// Env: PRICE_FEED_RETRY_COUNT.
	PriceFeedMaxRetries int
	// PriceFeedRetryDelay is base delay between retries.
	// Env: PRICE_FEED_RETRY_DELAY_MS.
	PriceFeedRetryDelay time.Duration
	// PriceFeedMaxQuoteAge rejects upstream quotes whose as-of time is older than this (0 disables).
	// Env: PRICE_FEED_MAX_QUOTE_AGE_MS.
	PriceFeedMaxQuoteAge time.Duration
	// PriceFeedDedupWindow skips ingest when the same symbol price repeats within this window (0 disables).
	// Env: PRICE_FEED_DEDUP_WINDOW_MS.
	PriceFeedDedupWindow time.Duration

	// Twelve Data credentials and rate caps.
	// Env: PRICE_FEED_TWELVEDATA_API_KEY, PRICE_FEED_TWELVEDATA_RATE_LIMIT_RPM.
	PriceFeedTwelveDataAPIKey       string
	PriceFeedTwelveDataRateLimitRPM int
	// AuthSessionTTL controls backend session expiry.
	// Env: AUTH_SESSION_TTL_SECONDS.
	AuthSessionTTL time.Duration
	// AuthCookieSecure toggles Secure flag on auth cookie.
	// Env: AUTH_COOKIE_SECURE.
	AuthCookieSecure bool
}

func Load() (Config, error) {
	wmMs := getEnvInt("ORDERING_WATERMARK_MS", defaultOrderingWatermarkMS)
	if wmMs < 0 {
		wmMs = defaultOrderingWatermarkMS
	}
	tickMs := getEnvInt("APPLY_WORKER_TICK_MS", defaultApplyWorkerTickMS)
	if tickMs < 1 {
		tickMs = defaultApplyWorkerTickMS
	}
	workers := getEnvInt("APPLY_WORKER_COUNT", defaultApplyWorkerCount)
	if workers < 1 {
		workers = 1
	}
	priceNS, err := uuid.Parse(getEnv("PRICE_STREAM_PORTFOLIO_ID", defaultPriceStreamPortfolioID))
	if err != nil {
		priceNS = uuid.MustParse(defaultPriceStreamPortfolioID)
	}
	priceShards := getEnvInt("PRICE_STREAM_SHARD_COUNT", defaultPriceStreamShards)
	if priceShards < 1 {
		priceShards = defaultPriceStreamShards
	}
	priceWorkers := getEnvInt("PRICE_APPLY_WORKER_COUNT", defaultPriceApplyWorkers)
	if priceWorkers < 1 {
		priceWorkers = 1
	}
	riskDebounceMs := getEnvInt("RISK_RECOMPUTE_DEBOUNCE_MS", defaultRiskRecomputeDebounceMS)
	if riskDebounceMs < 100 {
		riskDebounceMs = 100
	}
	if riskDebounceMs > 500 {
		riskDebounceMs = 500
	}
	riskWindowN := getEnvInt("RISK_SIGMA_WINDOW_N", defaultRiskSigmaWindowN)
	if riskWindowN < 2 {
		riskWindowN = defaultRiskSigmaWindowN
	}
	snapshotEnabled := getEnvBool("SNAPSHOT_ENABLED", true)
	portfolioSnapN := getEnvIntFromKeys([]string{"SNAPSHOT_EVERY_N_EVENTS", "PORTFOLIO_SNAPSHOT_MIN_EVENTS"}, 0)
	if portfolioSnapN < 0 {
		portfolioSnapN = 0
	}
	portfolioSnapSec := getEnvIntFromKeys([]string{"SNAPSHOT_MIN_INTERVAL_SEC", "PORTFOLIO_SNAPSHOT_INTERVAL_SEC"}, 0)
	if portfolioSnapSec < 0 {
		portfolioSnapSec = 0
	}
	if !snapshotEnabled {
		portfolioSnapN = 0
		portfolioSnapSec = 0
	}
	maxEventAgeMs := getEnvInt("ORDERING_MAX_EVENT_AGE_MS", 0)
	if maxEventAgeMs < 0 {
		maxEventAgeMs = 0
	}
	partitions := DerivePriceStreamPartitions(priceNS, priceShards)

	ingestRPS := getEnvInt("HTTP_RATE_LIMIT_INGEST_RPS", 20)
	if ingestRPS < 1 {
		ingestRPS = 20
	}
	ingestBurst := getEnvInt("HTTP_RATE_LIMIT_INGEST_BURST", ingestRPS*2)
	if ingestBurst < 1 {
		ingestBurst = ingestRPS * 2
	}
	getRPS := getEnvInt("HTTP_RATE_LIMIT_GET_RPS", 60)
	if getRPS < 1 {
		getRPS = 60
	}
	getBurst := getEnvInt("HTTP_RATE_LIMIT_GET_BURST", getRPS*2)
	if getBurst < 1 {
		getBurst = getRPS * 2
	}

	openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	openAIBase := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	openAIModel := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	feedProvider := strings.ToLower(strings.TrimSpace(getEnv("PRICE_FEED_PROVIDER", defaultPriceFeedProvider)))
	if feedProvider == "" || feedProvider != defaultPriceFeedProvider {
		feedProvider = defaultPriceFeedProvider
	}
	feedPollSeconds := getEnvInt("PRICE_FEED_POLL_SECONDS", defaultPriceFeedPollSeconds)
	if feedPollSeconds < 1 {
		feedPollSeconds = defaultPriceFeedPollSeconds
	}
	feedTimeoutMS := getEnvInt("PRICE_FEED_HTTP_TIMEOUT_MS", defaultPriceFeedHTTPTimeoutMS)
	if feedTimeoutMS < 100 {
		feedTimeoutMS = defaultPriceFeedHTTPTimeoutMS
	}
	feedRetryCount := getEnvInt("PRICE_FEED_RETRY_COUNT", defaultPriceFeedRetryCount)
	if feedRetryCount < 0 {
		feedRetryCount = 0
	}
	feedRetryDelayMS := getEnvInt("PRICE_FEED_RETRY_DELAY_MS", defaultPriceFeedRetryDelayMS)
	if feedRetryDelayMS < 0 {
		feedRetryDelayMS = defaultPriceFeedRetryDelayMS
	}
	maxQuoteAgeMS := getEnvInt("PRICE_FEED_MAX_QUOTE_AGE_MS", defaultPriceFeedMaxQuoteAgeMS)
	if maxQuoteAgeMS < 0 {
		maxQuoteAgeMS = 0
	}
	dedupWindowMS := getEnvInt("PRICE_FEED_DEDUP_WINDOW_MS", defaultPriceFeedDedupWindowMS)
	if dedupWindowMS < 0 {
		dedupWindowMS = 0
	}
	twelveDataRPM := getEnvInt("PRICE_FEED_TWELVEDATA_RATE_LIMIT_RPM", 8)
	if twelveDataRPM < 1 {
		twelveDataRPM = 8
	}
	authTTLSec := getEnvInt("AUTH_SESSION_TTL_SECONDS", defaultAuthSessionTTLSec)
	if authTTLSec < 300 {
		authTTLSec = defaultAuthSessionTTLSec
	}
	feedSymbols := parseCSVSymbols(os.Getenv("PRICE_FEED_SYMBOLS"))
	feedPollSeconds = applyTwelveDataRateLimitSafety(feedPollSeconds, len(feedSymbols), twelveDataRPM)

	cfg := Config{
		Port:                            getEnv("PORT", defaultPort),
		DatabaseURL:                     os.Getenv("DATABASE_URL"),
		ShutdownTimeout:                 time.Duration(getEnvInt("SHUTDOWN_TIMEOUT_SECONDS", defaultShutdownTimeoutSecond)) * time.Second,
		OrderingWatermark:               time.Duration(wmMs) * time.Millisecond,
		OrderingMaxEventAge:             time.Duration(maxEventAgeMs) * time.Millisecond,
		ApplyWorkerTick:                 time.Duration(tickMs) * time.Millisecond,
		ApplyWorkerCount:                workers,
		PriceStreamNamespace:            priceNS,
		PriceStreamPartitions:           partitions,
		PriceStreamShardCount:           priceShards,
		PriceApplyWorkerCount:           priceWorkers,
		RiskRecomputeDebounce:           time.Duration(riskDebounceMs) * time.Millisecond,
		RiskSigmaWindowN:                riskWindowN,
		RiskSnapshotWriteEnabled:        getEnvBool("RISK_SNAPSHOT_WRITE_ENABLED", false),
		SnapshotEnabled:                 snapshotEnabled,
		PortfolioSnapshotMinEvents:      portfolioSnapN,
		PortfolioSnapshotInterval:       time.Duration(portfolioSnapSec) * time.Second,
		RateLimitIngestEnabled:          getEnvBool("HTTP_RATE_LIMIT_INGEST_ENABLED", false),
		RateLimitIngestRPS:              ingestRPS,
		RateLimitIngestBurst:            ingestBurst,
		RateLimitGetEnabled:             getEnvBool("HTTP_RATE_LIMIT_GET_ENABLED", false),
		RateLimitGetRPS:                 getRPS,
		RateLimitGetBurst:               getBurst,
		OpenAIAPIKey:                    openAIKey,
		OpenAIBaseURL:                   openAIBase,
		OpenAIModel:                     openAIModel,
		PrometheusEnabled:               getEnvBool("PROMETHEUS_ENABLED", false),
		PriceFeedEnabled:                getEnvBool("PRICE_FEED_ENABLED", false),
		PriceFeedProvider:               feedProvider,
		PriceFeedPollInterval:           time.Duration(feedPollSeconds) * time.Second,
		PriceFeedSymbols:                feedSymbols,
		PriceFeedHTTPTimeout:            time.Duration(feedTimeoutMS) * time.Millisecond,
		PriceFeedMaxRetries:             feedRetryCount,
		PriceFeedRetryDelay:             time.Duration(feedRetryDelayMS) * time.Millisecond,
		PriceFeedMaxQuoteAge:            time.Duration(maxQuoteAgeMS) * time.Millisecond,
		PriceFeedDedupWindow:            time.Duration(dedupWindowMS) * time.Millisecond,
		PriceFeedTwelveDataAPIKey:       strings.TrimSpace(os.Getenv("PRICE_FEED_TWELVEDATA_API_KEY")),
		PriceFeedTwelveDataRateLimitRPM: twelveDataRPM,
		AuthSessionTTL:                  time.Duration(authTTLSec) * time.Second,
		AuthCookieSecure:                getEnvBool("AUTH_COOKIE_SECURE", false),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "TRUE", "True", "yes", "YES":
		return true
	case "0", "false", "FALSE", "False", "no", "NO":
		return false
	default:
		return fallback
	}
}

func getEnvInt(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

// getEnvIntFromKeys returns the first successfully parsed int from the given env keys (in order).
func getEnvIntFromKeys(keys []string, fallback int) int {
	for _, key := range keys {
		raw := os.Getenv(key)
		if raw == "" {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			continue
		}
		return value
	}
	return fallback
}

func parseCSVSymbols(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		s := strings.ToUpper(strings.TrimSpace(part))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// applyTwelveDataRateLimitSafety raises poll interval when needed so
// request rate (assuming one request per symbol per poll) does not exceed RPM.
func applyTwelveDataRateLimitSafety(pollSeconds, symbolCount, rpm int) int {
	if pollSeconds < 1 {
		pollSeconds = 1
	}
	if symbolCount < 1 || rpm < 1 {
		return pollSeconds
	}
	minPollSeconds := ((symbolCount * 60) + rpm - 1) / rpm // ceil(symbolCount*60/rpm)
	if minPollSeconds < 1 {
		minPollSeconds = 1
	}
	if pollSeconds < minPollSeconds {
		return minPollSeconds
	}
	return pollSeconds
}

// DerivePriceStreamPartitions returns deterministic synthetic events.portfolio_id values for
// sharded price ingestion. All share the same namespace UUID (PRICE_STREAM_PORTFOLIO_ID).
func DerivePriceStreamPartitions(namespace uuid.UUID, shardCount int) []uuid.UUID {
	if shardCount < 1 {
		shardCount = 1
	}
	out := make([]uuid.UUID, shardCount)
	for i := 0; i < shardCount; i++ {
		out[i] = uuid.NewSHA1(namespace, []byte("v1-price-partition\x00"+strconv.Itoa(i)))
	}
	return out
}

// PricePartitionForSymbol picks a stable partition for a ticker (ingest routing).
func PricePartitionForSymbol(partitions []uuid.UUID, symbol string) (uuid.UUID, error) {
	if len(partitions) == 0 {
		return uuid.Nil, fmt.Errorf("no price partitions configured")
	}
	h := uint32(2166136261)
	for i := 0; i < len(symbol); i++ {
		h ^= uint32(symbol[i])
		h *= 16777619
	}
	return partitions[h%uint32(len(partitions))], nil
}
