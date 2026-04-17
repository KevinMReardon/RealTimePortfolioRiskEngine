package pricesource

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// PriceQuote is normalized quote output from any provider.
// Symbol is always normalized to the internal PricePayload symbol policy.
type PriceQuote struct {
	Symbol         string
	ProviderSymbol string
	Price          decimal.Decimal
	Currency       string
	AsOf           time.Time
	SourceSequence int64
}

// FetchResult is a normalized batch fetch result.
type FetchResult struct {
	Provider  string
	FetchedAt time.Time
	Quotes    []PriceQuote
	Partial   bool
}

// HealthMetadata is provider health/status information for observability.
type HealthMetadata struct {
	Provider         string
	Healthy          bool
	CheckedAt        time.Time
	LastFetchLatency time.Duration
	LastError        string
	RateLimitRPM     int
	LastRequestCount int
	LastSuccessAt    *time.Time
}

// PriceProvider fetches provider quotes and exposes health metadata.
type PriceProvider interface {
	Name() string
	FetchQuotes(ctx context.Context, symbols []string) (FetchResult, error)
	Health() HealthMetadata
}

// ProviderError classifies provider failures for retry/failover policy.
type ProviderError struct {
	Provider   string
	StatusCode int
	Retryable  bool
	Err        error
}

func (e ProviderError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s status %d: %v", e.Provider, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Provider, e.Err)
}

func (e ProviderError) Unwrap() error {
	return e.Err
}
