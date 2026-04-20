package alpaca

import (
	"context"
	"fmt"

	sdkmarket "github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	sdkstream "github.com/alpacahq/alpaca-trade-api-go/v3/marketdata/stream"
)

// StocksStream wraps sdkstream.StocksClient with normalized StreamBar callbacks.
// Connect blocks until ctx is cancelled or the stream terminates — run it in its own goroutine.
type StocksStream struct {
	inner *sdkstream.StocksClient
}

// NewStocksStream builds a stock market data stream client (WebSocket).
// Feed selects SIP / IEX / etc.; credentials are the same API keys as REST.
func NewStocksStream(cfg StreamConfig) (*StocksStream, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	feed := cfg.Feed
	if feed == "" {
		feed = sdkmarket.IEX
	}
	base := cfg.DataStreamBaseURL
	if base == "" {
		base = DefaultDataStreamBaseURL
	}
	cli := sdkstream.NewStocksClient(feed,
		sdkstream.WithBaseURL(base),
		sdkstream.WithCredentials(cfg.KeyID, cfg.SecretKey),
	)
	return &StocksStream{inner: cli}, nil
}

// Connect establishes (and maintains) the WebSocket per the upstream SDK semantics.
func (s *StocksStream) Connect(ctx context.Context) error {
	if s == nil || s.inner == nil {
		return fmt.Errorf("alpaca StocksStream: nil client")
	}
	return s.inner.Connect(ctx)
}

// SubscribeMinuteBars registers bar callbacks and subscribes to symbols (minute aggregates).
func (s *StocksStream) SubscribeMinuteBars(handler func(StreamBar), symbols ...string) error {
	if s == nil || s.inner == nil {
		return fmt.Errorf("alpaca StocksStream: nil client")
	}
	if handler == nil {
		handler = func(StreamBar) {}
	}
	return s.inner.SubscribeToBars(func(b sdkstream.Bar) {
		handler(StreamBar{
			Symbol:    b.Symbol,
			Open:      b.Open,
			High:      b.High,
			Low:       b.Low,
			Close:     b.Close,
			Volume:    b.Volume,
			VWAP:      b.VWAP,
			Timestamp: b.Timestamp,
		})
	}, symbols...)
}

// Terminated reports irrecoverable stream shutdown (same as upstream SDK).
func (s *StocksStream) Terminated() <-chan error {
	if s == nil || s.inner == nil {
		ch := make(chan error, 1)
		close(ch)
		return ch
	}
	return s.inner.Terminated()
}
