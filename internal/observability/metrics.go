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
