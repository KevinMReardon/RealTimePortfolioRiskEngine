package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestMetricsRoute_Disabled(t *testing.T) {
	t.Parallel()
	r := NewRouter(RouterConfig{
		Logger:            zap.NewNop(),
		PrometheusEnabled: false,
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMetricsRoute_Enabled(t *testing.T) {
	t.Parallel()
	r := NewRouter(RouterConfig{
		Logger:            zap.NewNop(),
		PrometheusEnabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	for _, metricName := range []string{
		"events_appended_total",
		"dlq_events_total",
		"projection_lag_seconds",
	} {
		if !strings.Contains(body, metricName) {
			t.Fatalf("metrics output missing %q", metricName)
		}
	}
}
