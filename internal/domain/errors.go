package domain

import "errors"

var (
	// ErrValidation is the sentinel error for all domain/input validation failures.
	ErrValidation = errors.New("domain validation error")

	// ErrPositionUnderflow is returned when applying a trade would result in
	// a negative position (no shorting rule).
	ErrPositionUnderflow = errors.New("position underflow")

	// Event-level domain errors. These may be returned alongside ErrValidation
	// (wrapped with additional context) or on their own where appropriate.
	ErrInvalidPayload  = errors.New("invalid payload")
	ErrDuplicateEvent  = errors.New("duplicate event")
	ErrOutOfOrderEvent = errors.New("out of order event")
	ErrInvalidEvent    = errors.New("invalid event")
	ErrUnknownType     = errors.New("unknown event type")
)
