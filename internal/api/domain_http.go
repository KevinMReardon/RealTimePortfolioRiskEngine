package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// mapDomainErrors maps domain/ingestion errors to HTTP status, LLD §12 error_code, and a safe message.
func mapDomainErrors(err error) (status int, code string, message string, ok bool) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, ErrCodeValidation, trimAfterSentinel(err, domain.ErrValidation), true
	case errors.Is(err, domain.ErrInvalidPayload):
		return http.StatusBadRequest, ErrCodeValidation, trimAfterSentinel(err, domain.ErrInvalidPayload), true
	case errors.Is(err, domain.ErrInvalidEvent):
		return http.StatusBadRequest, ErrCodeValidation, trimAfterSentinel(err, domain.ErrInvalidEvent), true
	case errors.Is(err, domain.ErrUnknownType):
		return http.StatusBadRequest, ErrCodeValidation, trimAfterSentinel(err, domain.ErrUnknownType), true
	case errors.Is(err, domain.ErrPositionUnderflow):
		return http.StatusConflict, ErrCodePositionUnderflow, "trade would exceed position", true
	case errors.Is(err, domain.ErrDuplicateEvent):
		return http.StatusConflict, ErrCodeIdempotencyConflict, trimAfterSentinel(err, domain.ErrDuplicateEvent), true
	case errors.Is(err, domain.ErrOutOfOrderEvent):
		return http.StatusConflict, ErrCodeLateEventBeyondWatermark, trimAfterSentinel(err, domain.ErrOutOfOrderEvent), true
	default:
		return 0, "", "", false
	}
}

// MapDomainIngestError maps domain/ingestion errors to HTTP status, LLD §12 error_code, and a safe message.
func MapDomainIngestError(err error) (status int, code string, message string) {
	if s, c, m, ok := mapDomainErrors(err); ok {
		return s, c, m
	}
	return http.StatusInternalServerError, ErrCodeInternal, "internal error"
}

func trimAfterSentinel(err error, sentinel error) string {
	s := err.Error()
	p := sentinel.Error()
	if strings.HasPrefix(s, p+": ") {
		return strings.TrimSpace(s[len(p)+2:])
	}
	return strings.TrimSpace(s)
}
