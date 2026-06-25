package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/policy"
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
	defaultAlpacaBaseURL          = "https://paper-api.alpaca.markets"
	defaultAlpacaSyncIntervalSec  = 90
	defaultAlpacaSyncHistoryDays  = 365
	defaultAlpacaImportPollSec    = 2
	defaultAlpacaImportTimeoutSec = 7200
	defaultAlpacaDataBaseURL      = "https://data.alpaca.markets"
	defaultPriceFeedAlpacaRPM     = 200
	defaultAgentBriefingCron      = "0 9-16 * * 1-5"
	defaultAgentBriefingTZ        = "America/New_York"
	defaultAgentBriefingCooldownM = 30
	defaultAgentModel             = "claude-sonnet-4.6"
	defaultAgentMaxTokens         = 2048
	defaultAgentTemperature       = 0.2
	defaultAgentMaxToolCalls      = 12
	defaultAgentMaxTurns          = 8
	defaultAgentSessionTimeoutSec = 120
	defaultPolicyMode             = "enforce"

	// Phase 3 autonomous execution (AGENT_EXEC_MODE); orchestration is wired in later PRs.
	defaultAgentPaperAutoTimeoutSec         = 300 // 5 minutes
	defaultAgentMaxAutoSubmitsPerSession    = 5
	defaultEquityAnchorEnsureIntervalMin    = 15
)

// Agent execution modes for AGENT_EXEC_MODE (exact env strings; default off).
const (
	AgentExecModeOff       = "off"
	AgentExecModePaperAuto = "paper_auto"
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
	// Env: PRICE_FEED_PROVIDER — "twelvedata" (default) or "alpaca" (Market Data REST polling).
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
	// PriceFeedAlpacaRateLimitRPM caps assumed requests-per-minute when PRICE_FEED_PROVIDER=alpaca
	// (one HTTP call per equity symbol per poll + batched crypto). Env: PRICE_FEED_ALPACA_RATE_LIMIT_RPM.
	PriceFeedAlpacaRateLimitRPM int
	// AuthSessionTTL controls backend session expiry.
	// Env: AUTH_SESSION_TTL_SECONDS.
	AuthSessionTTL time.Duration
	// AuthCookieSecure toggles Secure flag on auth cookie.
	// Env: AUTH_COOKIE_SECURE.
	AuthCookieSecure bool
	// SingleUserApp allows at most one catalog portfolio per signed-in user (and one catalog row without auth).
	// Env: SINGLE_USER_APP (default true).
	SingleUserApp bool

	// Alpaca Trading API credentials by mode.
	// Env:
	//   ALPACA_PAPER_KEY_ID / ALPACA_PAPER_SECRET_KEY / ALPACA_PAPER_BASE_URL
	//   ALPACA_LIVE_KEY_ID  / ALPACA_LIVE_SECRET_KEY  / ALPACA_LIVE_BASE_URL
	// Backward-compatible fallback:
	//   ALPACA_KEY_ID / ALPACA_SECRET_KEY / ALPACA_BASE_URL (treated as paper when mode vars are unset)
	AlpacaPaperKeyID     string
	AlpacaPaperSecretKey string
	AlpacaPaperBaseURL   string
	AlpacaLiveKeyID      string
	AlpacaLiveSecretKey  string
	AlpacaLiveBaseURL    string
	// AlpacaSyncDisabled is true when ALPACA_SYNC_ENABLED is explicitly false (0/false/no/off).
	// When unset, sync uses "auto": the fill sync worker runs when keys are set and a target portfolio exists
	// (explicit env or single-user resolve).
	// Env: ALPACA_SYNC_ENABLED (optional; set false to keep keys for import/backfill but disable polling sync).
	AlpacaSyncDisabled bool
	// AlpacaDataBaseURL is the Market Data REST API origin (distinct from trading REST and from the WS stream).
	// Defaults to https://data.alpaca.markets when ALPACA_DATA_BASE_URL is unset (used by PRICE_FEED_PROVIDER=alpaca).
	// Env: ALPACA_DATA_BASE_URL.
	AlpacaDataBaseURL string
	// AlpacaSyncInterval governs REST polling for new fills.
	// Env: ALPACA_SYNC_INTERVAL_SECONDS (default 90).
	AlpacaSyncInterval time.Duration
	// AlpacaSyncHistoryLookback seeds the first activities request when no stored watermark exists.
	// Env: ALPACA_SYNC_HISTORY_DAYS (default 365).
	AlpacaSyncHistoryLookback time.Duration
	// AlpacaImportJobPollInterval is how often the import worker tries to claim a queued job.
	// Env: ALPACA_IMPORT_JOB_POLL_SECONDS (default 2).
	AlpacaImportJobPollInterval time.Duration
	// AlpacaImportJobTimeout caps one BackfillFills run per job.
	// Env: ALPACA_IMPORT_JOB_TIMEOUT_SECONDS (default 7200).
	AlpacaImportJobTimeout time.Duration

	// AgentBriefingEnabled toggles Anthropic-backed briefing APIs.
	// Env: AGENT_BRIEFING_ENABLED.
	AgentBriefingEnabled bool
	// AgentBriefingSchedulerEnabled toggles scheduled daily briefings.
	// Env: AGENT_BRIEFING_SCHEDULER_ENABLED.
	AgentBriefingSchedulerEnabled bool
	// AgentBriefingCron is the 5-field cron schedule for scheduled briefings.
	// Env: AGENT_BRIEFING_CRON (default "0 9-16 * * 1-5", hourly on weekdays).
	AgentBriefingCron string
	// AgentBriefingTZ is IANA timezone for cron interpretation.
	// Env: AGENT_BRIEFING_TZ (default "America/New_York").
	AgentBriefingTZ string
	// AgentBriefingCooldown is the minimum wait between successful scheduled briefings.
	// Env: AGENT_BRIEFING_COOLDOWN_MINUTES (default 30; 0 disables cooldown).
	AgentBriefingCooldown time.Duration
	// AnthropicAPIKey from ANTHROPIC_API_KEY (trimmed). Empty disables provider calls.
	AnthropicAPIKey string
	// AnthropicBaseURL from ANTHROPIC_BASE_URL (optional).
	AnthropicBaseURL string
	// AgentModel from AGENT_MODEL (default claude-sonnet-4.6).
	AgentModel string
	// AgentMaxTokens from AGENT_MAX_TOKENS (default 2048, min 256).
	AgentMaxTokens int
	// AgentTemperature from AGENT_TEMPERATURE (default 0.2, clamped [0,1]).
	AgentTemperature float64
	// AgentMaxToolCalls from AGENT_MAX_TOOL_CALLS (default 12, min 1).
	AgentMaxToolCalls int
	// AgentMaxTurns from AGENT_MAX_TURNS (default 8, min 1).
	AgentMaxTurns int
	// AgentSessionTimeout from AGENT_SESSION_TIMEOUT_SECONDS (default 120, min 10s).
	AgentSessionTimeout time.Duration

	// --- Phase 3 agent execution (submit orchestration wired later; see AGENT_EXEC_MODE) ---
	// AgentExecMode is the effective mode after startup safety rules.
	// Env: AGENT_EXEC_MODE — AgentExecModeOff (default, unset or "off") or AgentExecModePaperAuto ("paper_auto").
	AgentExecMode string
	// AgentExecPaperAutoSuppressedDueToMonitorPolicy is set when paper_auto was requested but POLICY_MODE=monitor:
	// autonomous Alpaca submit must not run in monitor mode (violations become effective ALLOW). Load() forces
	// effective AgentExecMode to AgentExecModeOff and records this flag — fail-closed for Phase 3 auto-submit only.
	AgentExecPaperAutoSuppressedDueToMonitorPolicy bool
	// AgentCriticModel optional critic/review model id for Phase 3 (empty = server default when orchestration exists).
	// Env: AGENT_CRITIC_MODEL (default empty).
	AgentCriticModel string
	// AgentPaperAutoTimeout bounds one autonomous paper submit pipeline attempt (future orchestration).
	// Env: AGENT_PAPER_AUTO_TIMEOUT_SECONDS (default 300; min 10).
	AgentPaperAutoTimeout time.Duration
	// AgentMaxAutoSubmitsPerSession caps broker submits per agent session (future orchestration).
	// Env: AGENT_MAX_AUTO_SUBMITS_PER_SESSION (default 5; min 1).
	AgentMaxAutoSubmitsPerSession int
	// EquityAnchorEnsureInterval is how often the server retries missing NY-day equity anchors.
	// Env: EQUITY_ANCHOR_ENSURE_INTERVAL_MINUTES (default 15; min 1).
	EquityAnchorEnsureInterval time.Duration

	// --- Phase 2 proposals / policy-as-code (HTTP wiring optional; see PROPOSALS_ENABLED) ---
	// ProposalsEnabled toggles proposal persistence APIs when implemented.
	// Env: PROPOSALS_ENABLED (default false).
	ProposalsEnabled bool
	// TradingHalt is a process-level kill switch folded into policy Snapshot.KillSwitchEnv (OR with DB row).
	// Env: TRADING_HALT (default false).
	TradingHalt bool
	// PolicyMode selects enforce (block on violations) vs monitor (log violations, effective ALLOW).
	// Env: POLICY_MODE — "enforce" (default) or "monitor".
	PolicyMode policy.Mode
	// PolicySymbolWhitelist is a comma-separated list; if non-empty, only these symbols may trade.
	// Env: POLICY_SYMBOL_WHITELIST.
	PolicySymbolWhitelist []string
	// PolicySymbolBlacklist is a comma-separated list; listed symbols are denied.
	// Env: POLICY_SYMBOL_BLACKLIST.
	PolicySymbolBlacklist []string
	// PolicyMaxOrderNotionalUSD caps a single order (USD notional). Zero disables the check.
	// Env: POLICY_MAX_ORDER_NOTIONAL_USD.
	PolicyMaxOrderNotionalUSD decimal.Decimal
	// PolicyMaxDailyNotionalUSD caps cumulative notional per day. Zero disables.
	// Env: POLICY_MAX_DAILY_NOTIONAL_USD.
	PolicyMaxDailyNotionalUSD decimal.Decimal
	// PolicyMaxPositionPct is max post-trade position market value as % of portfolio equity. Zero disables.
	// Env: POLICY_MAX_POSITION_PCT.
	PolicyMaxPositionPct decimal.Decimal
	// PolicyMaxDailyLossPct is max day drawdown vs equity anchor as a positive percent (e.g. 2 = 2%). Zero disables.
	// Env: POLICY_MAX_DAILY_LOSS_PCT.
	PolicyMaxDailyLossPct decimal.Decimal
	// PolicyMaxOrdersPerMinute rate-limits order evaluation/submit frequency. Zero disables.
	// Env: POLICY_MAX_ORDERS_PER_MINUTE.
	PolicyMaxOrdersPerMinute int
	// PolicyVersion is an optional label embedded in policy_config_hash for audit.
	// Env: POLICY_VERSION.
	PolicyVersion string
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
	if feedProvider == "" {
		feedProvider = defaultPriceFeedProvider
	}
	switch feedProvider {
	case "twelvedata", "alpaca":
	default:
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
	alpacaFeedRPM := getEnvInt("PRICE_FEED_ALPACA_RATE_LIMIT_RPM", defaultPriceFeedAlpacaRPM)
	if alpacaFeedRPM < 1 {
		alpacaFeedRPM = defaultPriceFeedAlpacaRPM
	}
	authTTLSec := getEnvInt("AUTH_SESSION_TTL_SECONDS", defaultAuthSessionTTLSec)
	if authTTLSec < 300 {
		authTTLSec = defaultAuthSessionTTLSec
	}
	feedSymbols := parseCSVSymbols(os.Getenv("PRICE_FEED_SYMBOLS"))
	switch feedProvider {
	case "alpaca":
		feedPollSeconds = applyTwelveDataRateLimitSafety(feedPollSeconds, len(feedSymbols), alpacaFeedRPM)
	default:
		feedPollSeconds = applyTwelveDataRateLimitSafety(feedPollSeconds, len(feedSymbols), twelveDataRPM)
	}

	legacyAlpacaBase := strings.TrimSpace(os.Getenv("ALPACA_BASE_URL"))
	if legacyAlpacaBase == "" {
		legacyAlpacaBase = defaultAlpacaBaseURL
	}
	paperBase := strings.TrimSpace(os.Getenv("ALPACA_PAPER_BASE_URL"))
	if paperBase == "" {
		paperBase = legacyAlpacaBase
	}
	liveBase := strings.TrimSpace(os.Getenv("ALPACA_LIVE_BASE_URL"))
	if liveBase == "" {
		liveBase = "https://api.alpaca.markets"
	}
	paperKeyID := strings.TrimSpace(os.Getenv("ALPACA_PAPER_KEY_ID"))
	paperSecret := strings.TrimSpace(os.Getenv("ALPACA_PAPER_SECRET_KEY"))
	if paperKeyID == "" && paperSecret == "" {
		paperKeyID = strings.TrimSpace(os.Getenv("ALPACA_KEY_ID"))
		paperSecret = strings.TrimSpace(os.Getenv("ALPACA_SECRET_KEY"))
	}
	liveKeyID := strings.TrimSpace(os.Getenv("ALPACA_LIVE_KEY_ID"))
	liveSecret := strings.TrimSpace(os.Getenv("ALPACA_LIVE_SECRET_KEY"))
	alpacaSyncSec := getEnvInt("ALPACA_SYNC_INTERVAL_SECONDS", defaultAlpacaSyncIntervalSec)
	if alpacaSyncSec < 30 {
		alpacaSyncSec = 30
	}
	if alpacaSyncSec > 86400 {
		alpacaSyncSec = 86400
	}
	alpacaHistDays := getEnvInt("ALPACA_SYNC_HISTORY_DAYS", defaultAlpacaSyncHistoryDays)
	if alpacaHistDays < 1 {
		alpacaHistDays = defaultAlpacaSyncHistoryDays
	}
	alpacaSyncDisabled := !getEnvBool("ALPACA_SYNC_ENABLED", true)
	alpacaDataBaseURL := strings.TrimSpace(os.Getenv("ALPACA_DATA_BASE_URL"))
	if alpacaDataBaseURL == "" {
		alpacaDataBaseURL = defaultAlpacaDataBaseURL
	}
	alpacaImportPollSec := getEnvInt("ALPACA_IMPORT_JOB_POLL_SECONDS", defaultAlpacaImportPollSec)
	if alpacaImportPollSec < 1 {
		alpacaImportPollSec = 1
	}
	alpacaImportTimeoutSec := getEnvInt("ALPACA_IMPORT_JOB_TIMEOUT_SECONDS", defaultAlpacaImportTimeoutSec)
	if alpacaImportTimeoutSec < 60 {
		alpacaImportTimeoutSec = 60
	}
	agentBriefingCron := strings.TrimSpace(getEnv("AGENT_BRIEFING_CRON", defaultAgentBriefingCron))
	if agentBriefingCron == "" {
		agentBriefingCron = defaultAgentBriefingCron
	}
	agentBriefingTZ := strings.TrimSpace(getEnv("AGENT_BRIEFING_TZ", defaultAgentBriefingTZ))
	if agentBriefingTZ == "" {
		agentBriefingTZ = defaultAgentBriefingTZ
	}
	agentBriefingCooldownMins := getEnvInt("AGENT_BRIEFING_COOLDOWN_MINUTES", defaultAgentBriefingCooldownM)
	if agentBriefingCooldownMins < 0 {
		agentBriefingCooldownMins = 0
	}
	agentModel := strings.TrimSpace(getEnv("AGENT_MODEL", defaultAgentModel))
	if agentModel == "" {
		agentModel = defaultAgentModel
	}
	agentMaxTokens := getEnvInt("AGENT_MAX_TOKENS", defaultAgentMaxTokens)
	if agentMaxTokens < 256 {
		agentMaxTokens = 256
	}
	agentTemperature := getEnvFloat64("AGENT_TEMPERATURE", defaultAgentTemperature)
	if agentTemperature < 0 {
		agentTemperature = 0
	}
	if agentTemperature > 1 {
		agentTemperature = 1
	}
	agentMaxToolCalls := getEnvInt("AGENT_MAX_TOOL_CALLS", defaultAgentMaxToolCalls)
	if agentMaxToolCalls < 1 {
		agentMaxToolCalls = 1
	}
	agentMaxTurns := getEnvInt("AGENT_MAX_TURNS", defaultAgentMaxTurns)
	if agentMaxTurns < 1 {
		agentMaxTurns = 1
	}
	agentSessionTimeoutSec := getEnvInt("AGENT_SESSION_TIMEOUT_SECONDS", defaultAgentSessionTimeoutSec)
	if agentSessionTimeoutSec < 10 {
		agentSessionTimeoutSec = 10
	}

	policyModeRaw := strings.ToLower(strings.TrimSpace(getEnv("POLICY_MODE", defaultPolicyMode)))
	var policyMode policy.Mode
	switch policyModeRaw {
	case "monitor":
		policyMode = policy.ModeMonitor
	default:
		policyMode = policy.ModeEnforce
	}

	execRaw := strings.ToLower(strings.TrimSpace(os.Getenv("AGENT_EXEC_MODE")))
	if execRaw == "" {
		execRaw = AgentExecModeOff
	}
	var execRequested string
	switch execRaw {
	case AgentExecModeOff:
		execRequested = AgentExecModeOff
	case AgentExecModePaperAuto:
		execRequested = AgentExecModePaperAuto
	default:
		return Config{}, fmt.Errorf(
			"AGENT_EXEC_MODE: invalid %q (use %q or %q)",
			execRaw,
			AgentExecModeOff,
			AgentExecModePaperAuto,
		)
	}
	execEffective := execRequested
	execSuppressedByMonitor := false
	if execRequested == AgentExecModePaperAuto && policyMode == policy.ModeMonitor {
		// Fail-closed for autonomous submit: POLICY_MODE=monitor must not pair with paper_auto (would allow trades
		// policy marked DENY). We downgrade to off instead of failing process startup so briefing/APIs still run.
		execEffective = AgentExecModeOff
		execSuppressedByMonitor = true
	}

	agentCriticModel := strings.TrimSpace(os.Getenv("AGENT_CRITIC_MODEL"))
	agentPaperAutoTimeoutSec := getEnvInt("AGENT_PAPER_AUTO_TIMEOUT_SECONDS", defaultAgentPaperAutoTimeoutSec)
	if agentPaperAutoTimeoutSec < 10 {
		agentPaperAutoTimeoutSec = defaultAgentPaperAutoTimeoutSec
	}
	if agentPaperAutoTimeoutSec > 86400 {
		agentPaperAutoTimeoutSec = 86400
	}
	agentMaxAutoSubmits := getEnvInt("AGENT_MAX_AUTO_SUBMITS_PER_SESSION", defaultAgentMaxAutoSubmitsPerSession)
	if agentMaxAutoSubmits < 1 {
		agentMaxAutoSubmits = defaultAgentMaxAutoSubmitsPerSession
	}
	equityAnchorEnsureMin := getEnvInt("EQUITY_ANCHOR_ENSURE_INTERVAL_MINUTES", defaultEquityAnchorEnsureIntervalMin)
	if equityAnchorEnsureMin < 1 {
		equityAnchorEnsureMin = defaultEquityAnchorEnsureIntervalMin
	}

	policyMaxOrdersPerMinute := getEnvInt("POLICY_MAX_ORDERS_PER_MINUTE", 0)
	if policyMaxOrdersPerMinute < 0 {
		policyMaxOrdersPerMinute = 0
	}

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
		PriceFeedAlpacaRateLimitRPM:     alpacaFeedRPM,
		AuthSessionTTL:                  time.Duration(authTTLSec) * time.Second,
		AuthCookieSecure:                getEnvBool("AUTH_COOKIE_SECURE", false),
		SingleUserApp:                   getEnvBool("SINGLE_USER_APP", true),

		AlpacaPaperKeyID:            paperKeyID,
		AlpacaPaperSecretKey:        paperSecret,
		AlpacaPaperBaseURL:          paperBase,
		AlpacaLiveKeyID:             liveKeyID,
		AlpacaLiveSecretKey:         liveSecret,
		AlpacaLiveBaseURL:           liveBase,
		AlpacaSyncDisabled:          alpacaSyncDisabled,
		AlpacaDataBaseURL:           alpacaDataBaseURL,
		AlpacaSyncInterval:          time.Duration(alpacaSyncSec) * time.Second,
		AlpacaSyncHistoryLookback:   time.Duration(alpacaHistDays) * 24 * time.Hour,
		AlpacaImportJobPollInterval: time.Duration(alpacaImportPollSec) * time.Second,
		AlpacaImportJobTimeout:      time.Duration(alpacaImportTimeoutSec) * time.Second,
		AgentBriefingEnabled:        getEnvBool("AGENT_BRIEFING_ENABLED", false),
		AgentBriefingSchedulerEnabled: getEnvBool(
			"AGENT_BRIEFING_SCHEDULER_ENABLED",
			false,
		),
		AgentBriefingCron:     agentBriefingCron,
		AgentBriefingTZ:       agentBriefingTZ,
		AgentBriefingCooldown: time.Duration(agentBriefingCooldownMins) * time.Minute,
		AnthropicAPIKey:       strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicBaseURL:      strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")),
		AgentModel:            agentModel,
		AgentMaxTokens:        agentMaxTokens,
		AgentTemperature:      agentTemperature,
		AgentMaxToolCalls:     agentMaxToolCalls,
		AgentMaxTurns:         agentMaxTurns,
		AgentSessionTimeout:   time.Duration(agentSessionTimeoutSec) * time.Second,

		AgentExecMode: execEffective,
		AgentExecPaperAutoSuppressedDueToMonitorPolicy: execSuppressedByMonitor,
		AgentCriticModel:              agentCriticModel,
		AgentPaperAutoTimeout:         time.Duration(agentPaperAutoTimeoutSec) * time.Second,
		AgentMaxAutoSubmitsPerSession: agentMaxAutoSubmits,
		EquityAnchorEnsureInterval:    time.Duration(equityAnchorEnsureMin) * time.Minute,

		ProposalsEnabled:          getEnvBool("PROPOSALS_ENABLED", false),
		TradingHalt:               getEnvBool("TRADING_HALT", false),
		PolicyMode:                policyMode,
		PolicySymbolWhitelist:     parseCSVSymbols(os.Getenv("POLICY_SYMBOL_WHITELIST")),
		PolicySymbolBlacklist:     parseCSVSymbols(os.Getenv("POLICY_SYMBOL_BLACKLIST")),
		PolicyMaxOrderNotionalUSD: getEnvDecimal("POLICY_MAX_ORDER_NOTIONAL_USD", decimal.Zero),
		PolicyMaxDailyNotionalUSD: getEnvDecimal("POLICY_MAX_DAILY_NOTIONAL_USD", decimal.Zero),
		PolicyMaxPositionPct:      getEnvDecimal("POLICY_MAX_POSITION_PCT", decimal.Zero),
		PolicyMaxDailyLossPct:     getEnvDecimal("POLICY_MAX_DAILY_LOSS_PCT", decimal.Zero),
		PolicyMaxOrdersPerMinute:  policyMaxOrdersPerMinute,
		PolicyVersion:             strings.TrimSpace(os.Getenv("POLICY_VERSION")),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	return cfg, nil
}

// AlpacaWorkerEnabled reports whether any Alpaca mode credentials are configured and sync is enabled.
// Portfolio-level credentials/selection comes from catalog mapping.
func (c Config) AlpacaWorkerEnabled() bool {
	if c.AlpacaSyncDisabled {
		return false
	}
	return true
}

// AlpacaImportJobsEnabled reports whether Alpaca REST is configured (async import jobs + worker).
func (c Config) AlpacaImportJobsEnabled() bool {
	hasPaper := strings.TrimSpace(c.AlpacaPaperKeyID) != "" && strings.TrimSpace(c.AlpacaPaperSecretKey) != ""
	hasLive := strings.TrimSpace(c.AlpacaLiveKeyID) != "" && strings.TrimSpace(c.AlpacaLiveSecretKey) != ""
	return hasPaper || hasLive
}

// AgentBriefingRuntimeEnabled reports whether briefing endpoints should be enabled.
func (c Config) AgentBriefingRuntimeEnabled() bool {
	return c.AgentBriefingEnabled
}

// AgentBriefingSchedulerRuntimeEnabled reports whether the scheduled runner should start.
func (c Config) AgentBriefingSchedulerRuntimeEnabled() bool {
	return c.AgentBriefingEnabled && c.AgentBriefingSchedulerEnabled
}

// ProposalsRuntimeEnabled reports whether Phase 2 proposal store and related paths should be active.
func (c Config) ProposalsRuntimeEnabled() bool {
	return c.ProposalsEnabled
}

// AgentPaperAutoRuntimeEnabled reports whether post-briefing autonomous paper submit is active.
func (c Config) AgentPaperAutoRuntimeEnabled() bool {
	return c.AgentExecMode == AgentExecModePaperAuto
}

// PolicyConfig builds internal/policy.Config from loaded env (for Evaluate and proposal persistence).
func (c Config) PolicyConfig() policy.Config {
	w := append([]string(nil), c.PolicySymbolWhitelist...)
	b := append([]string(nil), c.PolicySymbolBlacklist...)
	return policy.Config{
		Mode:                c.PolicyMode,
		SymbolWhitelist:     w,
		SymbolBlacklist:     b,
		MaxOrderNotionalUSD: c.PolicyMaxOrderNotionalUSD,
		MaxDailyNotionalUSD: c.PolicyMaxDailyNotionalUSD,
		MaxPositionPct:      c.PolicyMaxPositionPct,
		MaxDailyLossPct:     c.PolicyMaxDailyLossPct,
		MaxOrdersPerMinute:  c.PolicyMaxOrdersPerMinute,
		PolicyVersion:       c.PolicyVersion,
	}
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

func getEnvFloat64(key string, fallback float64) float64 {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

func getEnvDecimal(key string, fallback decimal.Decimal) decimal.Decimal {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	d, err := decimal.NewFromString(raw)
	if err != nil {
		return fallback
	}
	return d
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
