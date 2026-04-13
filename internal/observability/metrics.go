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
)

func ensureMetricsRegistered() {
	registerMetricsOnce.Do(func() {
		prometheus.MustRegister(eventsAppendedTotal, dlqEventsTotal, projectionLagSeconds)
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
