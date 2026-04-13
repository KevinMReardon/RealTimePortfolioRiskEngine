package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	RequestIDHeader = "X-Request-ID"
	RequestIDKey    = "request_id"
)

// RequestIDMiddleware ensures every request has a stable correlation ID.
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Header(RequestIDHeader, requestID)
		c.Set(RequestIDKey, requestID)
		c.Next()
	}
}

// RequestIDFromContext returns the request ID saved by middleware.
func RequestIDFromContext(c *gin.Context) string {
	if requestID, ok := c.Get(RequestIDKey); ok {
		if id, ok := requestID.(string); ok {
			return id
		}
	}
	return ""
}
