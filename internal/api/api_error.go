package api

import (
	"github.com/gin-gonic/gin"
)

// LLD §12 core error_code values (plus common HTTP extensions used by this service).
const (
	ErrCodeValidation               = "VALIDATION_ERROR"
	ErrCodeIdempotencyConflict      = "IDEMPOTENCY_CONFLICT"
	ErrCodeLateEventBeyondWatermark = "LATE_EVENT_BEYOND_WATERMARK"
	ErrCodePositionUnderflow        = "POSITION_UNDERFLOW"
	ErrCodeUnpricedPositionsPresent = "UNPRICED_POSITIONS_PRESENT"
	ErrCodeInsufficientData         = "INSUFFICIENT_DATA"
	ErrCodeInternal                 = "INTERNAL_ERROR"
	ErrCodeNotFound                 = "NOT_FOUND"
	ErrCodeRateLimited              = "RATE_LIMITED"
)

// APIError is the standard JSON error envelope (LLD §12).
type APIError struct {
	ErrorCode string         `json:"error_code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details"`
	RequestID string         `json:"request_id"`
}

// respondAPIError writes a §12-shaped error body and sets status. details may be nil (stored as {}).
func respondAPIError(c *gin.Context, status int, code, message string, details map[string]any) {
	if details == nil {
		details = map[string]any{}
	}
	c.JSON(status, APIError{
		ErrorCode: code,
		Message:   message,
		Details:   details,
		RequestID: RequestIDFromContext(c),
	})
}
