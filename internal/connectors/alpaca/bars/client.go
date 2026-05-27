// Package bars is a minimal Alpaca Market Data REST client for daily OHLCV bars used by the
// agent's get_daily_bars / get_technical_indicators / get_market_regime tools.
//
// API docs: https://docs.alpaca.markets/reference/stockbars
//
// Endpoint: GET {dataBaseURL}/v2/stocks/{symbol}/bars?timeframe=1Day&limit=N&adjustment=raw
// Auth: APCA-API-KEY-ID + APCA-API-SECRET-KEY headers.
package bars

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	defaultHTTPTimeout = 10 * time.Second
	defaultLimit       = 200
	maxLimit           = 1000
)

// Bar is one OHLCV daily bar (timestamp at session close).
type Bar struct {
	T time.Time
	O float64
	H float64
	L float64
	C float64
	V uint64
}

// Config holds connection parameters.
type Config struct {
	DataBaseURL string
	KeyID       string
	SecretKey   string
	HTTPTimeout time.Duration
	Log         *zap.Logger
}

// Client fetches daily bars from Alpaca.
type Client struct {
	cfg        Config
	httpClient *http.Client
	log        *zap.Logger
}

// New constructs a daily-bars client. Defaults DataBaseURL to the public Alpaca origin.
func New(cfg Config) *Client {
	if strings.TrimSpace(cfg.DataBaseURL) == "" {
		cfg.DataBaseURL = "https://data.alpaca.markets"
	}
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Client{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout},
		log:        log,
	}
}

// GetDailyBars returns up to `limit` daily bars for symbol, oldest first. limit defaults to 200.
func (c *Client) GetDailyBars(ctx context.Context, symbol string, limit int) ([]Bar, error) {
	if c == nil {
		return nil, fmt.Errorf("alpaca bars: nil client")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, fmt.Errorf("alpaca bars: symbol required")
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	q := url.Values{}
	q.Set("timeframe", "1Day")
	q.Set("limit", strconv.Itoa(limit))
	q.Set("adjustment", "raw")
	endpoint := strings.TrimRight(c.cfg.DataBaseURL, "/") + "/v2/stocks/" + url.PathEscape(symbol) + "/bars?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("alpaca bars: build request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", c.cfg.KeyID)
	req.Header.Set("APCA-API-SECRET-KEY", c.cfg.SecretKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alpaca bars: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("alpaca bars: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("alpaca bars: decode: %w", err)
	}
	out := make([]Bar, 0, len(parsed.Bars))
	for _, b := range parsed.Bars {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(b.T))
		if err != nil {
			continue
		}
		out = append(out, Bar{
			T: t,
			O: b.O,
			H: b.H,
			L: b.L,
			C: b.C,
			V: b.V,
		})
	}
	return out, nil
}

type apiResponse struct {
	Bars []apiBar `json:"bars"`
}

type apiBar struct {
	T string  `json:"t"`
	O float64 `json:"o"`
	H float64 `json:"h"`
	L float64 `json:"l"`
	C float64 `json:"c"`
	V uint64  `json:"v"`
}
