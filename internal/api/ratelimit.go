package api

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// PerIPRateLimiter is a token-bucket limiter keyed by client IP (see Gin ClientIP).
// One limiter instance should be used per route class (e.g. ingest vs read) so RPS/burst can differ.
type PerIPRateLimiter struct {
	limit rate.Limit
	burst int

	mu   sync.Mutex
	byIP map[string]*rate.Limiter
}

// NewPerIPRateLimiter returns a limiter with the given sustained rate (tokens per second) and burst size.
// rps and burst must be >= 1; otherwise they are clamped to 1.
func NewPerIPRateLimiter(rps, burst int) *PerIPRateLimiter {
	if rps < 1 {
		rps = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &PerIPRateLimiter{
		limit: rate.Limit(float64(rps)),
		burst: burst,
		byIP:  make(map[string]*rate.Limiter),
	}
}

// Allow reports whether one request from ip should be admitted (non-blocking).
func (p *PerIPRateLimiter) Allow(ip string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	lim, ok := p.byIP[ip]
	if !ok {
		lim = rate.NewLimiter(p.limit, p.burst)
		p.byIP[ip] = lim
	}
	return lim.Allow()
}

// PerIPRateLimitMiddleware returns Gin middleware that returns 429 with a §12 envelope when Allow(ip) is false.
// Pass nil to disable (no-op middleware).
func PerIPRateLimitMiddleware(l *PerIPRateLimiter) gin.HandlerFunc {
	if l == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		ip := c.ClientIP()
		if !l.Allow(ip) {
			c.Header("Retry-After", "1")
			respondAPIError(c, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded", map[string]any{
				"retry_after_seconds": 1,
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
