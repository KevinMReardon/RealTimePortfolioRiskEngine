package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPerIPRateLimitMiddleware_secondRequestFromSameIP_429(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	lim := NewPerIPRateLimiter(1, 1)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(PerIPRateLimitMiddleware(lim))
	r.POST("/v1/trades", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	const ip = "192.0.2.10"
	reqOK := httptest.NewRequest(http.MethodPost, "/v1/trades", nil)
	reqOK.RemoteAddr = ip + ":12345"
	recOK := httptest.NewRecorder()
	r.ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusNoContent {
		t.Fatalf("first status %d", recOK.Code)
	}

	req429 := httptest.NewRequest(http.MethodPost, "/v1/trades", nil)
	req429.RemoteAddr = ip + ":12345"
	rec429 := httptest.NewRecorder()
	r.ServeHTTP(rec429, req429)
	if rec429.Code != http.StatusTooManyRequests {
		t.Fatalf("second status %d body=%s", rec429.Code, rec429.Body.String())
	}
	var env APIError
	if err := json.Unmarshal(rec429.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.ErrorCode != ErrCodeRateLimited {
		t.Fatalf("error_code %q", env.ErrorCode)
	}
	if env.Message != "rate limit exceeded" {
		t.Fatalf("message %q", env.Message)
	}
	if env.Details == nil || env.Details["retry_after_seconds"] == nil {
		t.Fatalf("details %+v", env.Details)
	}
	if rec429.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After %q", rec429.Header().Get("Retry-After"))
	}
}

func TestPerIPRateLimitMiddleware_differentIPsIndependent(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	lim := NewPerIPRateLimiter(1, 1)
	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.Use(PerIPRateLimitMiddleware(lim))
	r.POST("/v1/trades", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, ip := range []string{"192.0.2.1", "192.0.2.2"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/trades", nil)
		req.RemoteAddr = ip + ":12345"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("ip %s status %d", ip, rec.Code)
		}
	}
}

func TestPerIPRateLimitMiddleware_nilLimiter_noop(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(PerIPRateLimitMiddleware(nil))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("iter %d status %d", i, rec.Code)
		}
	}
}
