package alpaca

import (
	"fmt"
	"os"
	"strings"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
)

const (
	// DefaultRESTBaseURLPaper is the Alpaca paper trading REST host.
	DefaultRESTBaseURLPaper = "https://paper-api.alpaca.markets"
	// DefaultRESTBaseURLLive is the Alpaca live trading REST host.
	DefaultRESTBaseURLLive = "https://api.alpaca.markets"
	// DefaultDataStreamBaseURL is the stock market data WebSocket base (path selects feed).
	DefaultDataStreamBaseURL = "https://stream.data.alpaca.markets/v2"
)

// RESTConfig selects the trading REST API (paper vs live) and credentials.
type RESTConfig struct {
	KeyID     string
	SecretKey string
	BaseURL   string
}

// Validate checks required fields for a functional REST client.
func (c RESTConfig) Validate() error {
	if strings.TrimSpace(c.KeyID) == "" || strings.TrimSpace(c.SecretKey) == "" {
		return fmt.Errorf("alpaca: KeyID and SecretKey are required")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return fmt.Errorf("alpaca: BaseURL is required")
	}
	return nil
}

// RESTConfigFromEnv loads ALPACA_KEY_ID, ALPACA_SECRET_KEY, ALPACA_BASE_URL (trimmed).
// Empty KeyID or SecretKey yields ok=false so callers can skip wiring when Alpaca is unused.
func RESTConfigFromEnv() (c RESTConfig, ok bool) {
	c.KeyID = strings.TrimSpace(os.Getenv("ALPACA_KEY_ID"))
	c.SecretKey = strings.TrimSpace(os.Getenv("ALPACA_SECRET_KEY"))
	c.BaseURL = strings.TrimSpace(os.Getenv("ALPACA_BASE_URL"))
	if c.BaseURL == "" {
		c.BaseURL = DefaultRESTBaseURLPaper
	}
	if c.KeyID == "" || c.SecretKey == "" {
		return c, false
	}
	return c, true
}

// StreamConfig configures the stock market data WebSocket client (distinct from REST BaseURL).
type StreamConfig struct {
	KeyID             string
	SecretKey         string
	DataStreamBaseURL string
	Feed              marketdata.Feed
}

// Validate checks credentials for streaming.
func (c StreamConfig) Validate() error {
	if strings.TrimSpace(c.KeyID) == "" || strings.TrimSpace(c.SecretKey) == "" {
		return fmt.Errorf("alpaca stream: KeyID and SecretKey are required")
	}
	return nil
}

// StreamConfigFromREST derives stream credentials from REST config and optional env overrides:
// ALPACA_DATA_STREAM_BASE_URL (default DefaultDataStreamBaseURL),
// ALPACA_DATA_FEED (iex, sip, otc, … — default iex).
func StreamConfigFromREST(rest RESTConfig) StreamConfig {
	rawFeed := strings.TrimSpace(os.Getenv("ALPACA_DATA_FEED"))
	feed := marketdata.Feed(strings.ToLower(rawFeed))
	if feed == "" {
		feed = marketdata.IEX
	}
	streamURL := strings.TrimSpace(os.Getenv("ALPACA_DATA_STREAM_BASE_URL"))
	if streamURL == "" {
		streamURL = DefaultDataStreamBaseURL
	}
	return StreamConfig{
		KeyID:             rest.KeyID,
		SecretKey:         rest.SecretKey,
		DataStreamBaseURL: streamURL,
		Feed:              feed,
	}
}
