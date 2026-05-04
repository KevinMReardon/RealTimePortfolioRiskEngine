package observability

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registerMetricsOnce sync.Once

	eventsAppendedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "events_appended_total",
		Help: "Total canonical events successfully appended to the event store.",
	})
	dlqEventsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "dlq_events_total",
		Help: "Total events written to dead-letter queue.",
	})
	projectionLagSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "projection_lag_seconds",
		Help: "Lag between now and the latest applied event_time.",
	}, []string{"pipeline"})
	priceFeedFetchLatencySeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "price_feed_fetch_latency_seconds",
		Help:    "Latency of provider fetch calls for the automated price feed.",
		Buckets: prometheus.DefBuckets,
	}, []string{"provider"})
	priceFeedSymbolsFetchedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "price_feed_symbols_fetched_total",
		Help: "Total quotes fetched from upstream providers.",
	}, []string{"provider"})
	priceFeedSymbolsIngestedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "price_feed_symbols_ingested_total",
		Help: "Total quotes ingested into canonical PriceUpdated events.",
	}, []string{"provider"})
	priceFeedDroppedStaleQuotesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "price_feed_dropped_stale_quotes_total",
		Help: "Total upstream quotes dropped because they exceeded staleness policy.",
	}, []string{"provider"})
	priceFeedDedupSkippedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "price_feed_dedup_skipped_total",
		Help: "Total quotes skipped because price was unchanged within the dedup window.",
	}, []string{"provider"})
	priceFeedProviderFailoversTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "price_feed_provider_failovers_total",
		Help: "Total provider failover events from one provider to the next.",
	}, []string{"from_provider", "to_provider"})
	priceFeedRateLimitHitsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "price_feed_rate_limit_hits_total",
		Help: "Total provider rate-limit (HTTP 429) responses seen by the feed runner.",
	}, []string{"provider"})
	alpacaSyncRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alpaca_sync_runs_total",
		Help: "Alpaca REST fill sync ticker outcomes.",
	}, []string{"result"})
	alpacaSyncFillOutcomesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "alpaca_sync_fill_outcomes_total",
		Help: "Per-fill ingestion outcomes during Alpaca sync.",
	}, []string{"result"})
	agentSessionOutcomesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_session_outcomes_total",
		Help: "Agent briefing session terminal outcomes.",
	}, []string{"status", "trigger_source"})
	agentToolCallsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_tool_calls_total",
		Help: "Agent tool call results.",
	}, []string{"tool_name", "status"})
	agentToolLatencySeconds = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agent_tool_latency_seconds",
		Help:    "Latency of agent tool calls.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tool_name"})
	agentValidationFailuresTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "agent_validation_failures_total",
		Help: "Agent output validation failures.",
	})
	agentTokenUsageTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agent_token_usage_total",
		Help: "Agent provider token usage.",
	}, []string{"direction"})

	// Phase 2: policy + proposed trades
	policyEvaluationsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "policy_evaluations_total",
		Help: "Policy Evaluate calls (effective outcome and primary rule code or none).",
	}, []string{"outcome", "rule_code"})
	policyKillSwitchBlocksTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "policy_kill_switch_blocks_total",
		Help: "Kill-switch violations recorded during policy evaluation.",
	}, []string{"source"})
	proposedTradeTransitionsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "proposed_trade_transitions_total",
		Help: "Proposed trade status transitions (e.g. insert, approve, deny).",
	}, []string{"from", "to"})
	proposalSubmitOutcomesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "proposal_submit_outcomes_total",
		Help: "POST /proposals/:id/submit outcomes (success, policy_denied, broker errors, etc.).",
	}, []string{"outcome"})
)

func ensureMetricsRegistered() {
	registerMetricsOnce.Do(func() {
		prometheus.MustRegister(
			eventsAppendedTotal,
			dlqEventsTotal,
			projectionLagSeconds,
			priceFeedFetchLatencySeconds,
			priceFeedSymbolsFetchedTotal,
			priceFeedSymbolsIngestedTotal,
			priceFeedDroppedStaleQuotesTotal,
			priceFeedDedupSkippedTotal,
			priceFeedProviderFailoversTotal,
			priceFeedRateLimitHitsTotal,
			alpacaSyncRunsTotal,
			alpacaSyncFillOutcomesTotal,
			agentSessionOutcomesTotal,
			agentToolCallsTotal,
			agentToolLatencySeconds,
			agentValidationFailuresTotal,
			agentTokenUsageTotal,
			policyEvaluationsTotal,
			policyKillSwitchBlocksTotal,
			proposedTradeTransitionsTotal,
			proposalSubmitOutcomesTotal,
		)
		projectionLagSeconds.WithLabelValues("trade").Set(0)
		projectionLagSeconds.WithLabelValues("price").Set(0)
	})
}

// MetricsHandler returns the Prometheus exposition handler.
func MetricsHandler() http.Handler {
	ensureMetricsRegistered()
	return promhttp.Handler()
}

// IncEventsAppended increments canonical successful appends.
func IncEventsAppended() {
	ensureMetricsRegistered()
	eventsAppendedTotal.Inc()
}

// IncDLQEvents increments dead-letter writes.
func IncDLQEvents() {
	ensureMetricsRegistered()
	dlqEventsTotal.Inc()
}

// ObserveProjectionLag updates the current projection lag from event time.
func ObserveProjectionLag(pipeline string, eventTime time.Time) {
	ensureMetricsRegistered()
	lag := time.Since(eventTime).Seconds()
	if lag < 0 {
		lag = 0
	}
	projectionLagSeconds.WithLabelValues(pipeline).Set(lag)
}

func ObservePriceFeedFetch(provider string, latency time.Duration, symbolsFetched int) {
	ensureMetricsRegistered()
	priceFeedFetchLatencySeconds.WithLabelValues(provider).Observe(latency.Seconds())
	if symbolsFetched > 0 {
		priceFeedSymbolsFetchedTotal.WithLabelValues(provider).Add(float64(symbolsFetched))
	}
}

func AddPriceFeedSymbolsIngested(provider string, n int) {
	ensureMetricsRegistered()
	if n > 0 {
		priceFeedSymbolsIngestedTotal.WithLabelValues(provider).Add(float64(n))
	}
}

func AddPriceFeedDroppedStaleQuotes(provider string, n int) {
	ensureMetricsRegistered()
	if n > 0 {
		priceFeedDroppedStaleQuotesTotal.WithLabelValues(provider).Add(float64(n))
	}
}

func AddPriceFeedDedupSkipped(provider string, n int) {
	ensureMetricsRegistered()
	if n > 0 {
		priceFeedDedupSkippedTotal.WithLabelValues(provider).Add(float64(n))
	}
}

func IncPriceFeedProviderFailover(fromProvider, toProvider string) {
	ensureMetricsRegistered()
	priceFeedProviderFailoversTotal.WithLabelValues(fromProvider, toProvider).Inc()
}

func IncPriceFeedRateLimitHit(provider string) {
	ensureMetricsRegistered()
	priceFeedRateLimitHitsTotal.WithLabelValues(provider).Inc()
}

// ObserveAlpacaSyncRun records one sync tick outcome: ok, error_before_fetch, error_during_tick.
func ObserveAlpacaSyncRun(result string) {
	ensureMetricsRegistered()
	if result == "" {
		result = "unknown"
	}
	alpacaSyncRunsTotal.WithLabelValues(result).Inc()
}

// ObserveAlpacaFillOutcome records how one FILL mapped to ingestion: appended, duplicate, skipped_invalid.
func ObserveAlpacaFillOutcome(result string) {
	ensureMetricsRegistered()
	if result == "" {
		result = "unknown"
	}
	alpacaSyncFillOutcomesTotal.WithLabelValues(result).Inc()
}

func ObserveAgentSessionOutcome(status, triggerSource string) {
	ensureMetricsRegistered()
	if status == "" {
		status = "unknown"
	}
	if triggerSource == "" {
		triggerSource = "unknown"
	}
	agentSessionOutcomesTotal.WithLabelValues(status, triggerSource).Inc()
}

func ObserveAgentToolCall(toolName, status string, latency time.Duration) {
	ensureMetricsRegistered()
	if toolName == "" {
		toolName = "unknown"
	}
	if status == "" {
		status = "unknown"
	}
	agentToolCallsTotal.WithLabelValues(toolName, status).Inc()
	if latency < 0 {
		latency = 0
	}
	agentToolLatencySeconds.WithLabelValues(toolName).Observe(latency.Seconds())
}

func IncAgentValidationFailure() {
	ensureMetricsRegistered()
	agentValidationFailuresTotal.Inc()
}

func AddAgentTokenUsage(inputTokens, outputTokens *int) {
	ensureMetricsRegistered()
	if inputTokens != nil && *inputTokens > 0 {
		agentTokenUsageTotal.WithLabelValues("input").Add(float64(*inputTokens))
	}
	if outputTokens != nil && *outputTokens > 0 {
		agentTokenUsageTotal.WithLabelValues("output").Add(float64(*outputTokens))
	}
}

// ObservePolicyEvaluation records one policy.Evaluate result (primary rule = first violation or "none").
func ObservePolicyEvaluation(effectiveOutcome, primaryRuleCode string, killSwitchBlocked bool, killSwitchSource string) {
	ensureMetricsRegistered()
	if effectiveOutcome == "" {
		effectiveOutcome = "unknown"
	}
	if primaryRuleCode == "" {
		primaryRuleCode = "none"
	}
	policyEvaluationsTotal.WithLabelValues(effectiveOutcome, primaryRuleCode).Inc()
	if killSwitchBlocked {
		src := killSwitchSource
		if src == "" {
			src = "unknown"
		}
		policyKillSwitchBlocksTotal.WithLabelValues(src).Inc()
	}
}

// IncProposedTradeTransition records a proposal row lifecycle transition.
func IncProposedTradeTransition(fromStatus, toStatus string) {
	ensureMetricsRegistered()
	if fromStatus == "" {
		fromStatus = "unknown"
	}
	if toStatus == "" {
		toStatus = "unknown"
	}
	proposedTradeTransitionsTotal.WithLabelValues(fromStatus, toStatus).Inc()
}

// ObserveProposalSubmit records a proposal submit HTTP outcome category.
func ObserveProposalSubmit(outcome string) {
	ensureMetricsRegistered()
	if outcome == "" {
		outcome = "unknown"
	}
	proposalSubmitOutcomesTotal.WithLabelValues(outcome).Inc()
}
