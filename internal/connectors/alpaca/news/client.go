// Package news is a minimal Alpaca Market News REST client for the agent's get_market_news tool.
//
// API docs: https://docs.alpaca.markets/reference/news-3
//
// Endpoint: GET {dataBaseURL}/v1beta1/news?symbols=AAPL,MSFT&limit=10&sort=desc
// Auth: APCA-API-KEY-ID + APCA-API-SECRET-KEY headers.
package news

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

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/agent"
)

const (
	defaultPath        = "/v1beta1/news"
	defaultLimit       = 10
	maxLimit           = 50
	defaultHTTPTimeout = 10 * time.Second
)

// Config holds the connection parameters for the Alpaca News client.
type Config struct {
	// DataBaseURL is the Alpaca Market Data REST origin, e.g. https://data.alpaca.markets.
	DataBaseURL string
	KeyID       string
	SecretKey   string
	// HTTPTimeout caps each request (default 10s).
	HTTPTimeout time.Duration
	Log         *zap.Logger
}

// Client is a thin HTTP wrapper around the Alpaca News API that satisfies agent.MarketNewsProvider.
type Client struct {
	cfg        Config
	httpClient *http.Client
	log        *zap.Logger
}

// New constructs a Client. If DataBaseURL is empty, defaults to the public market-data origin.
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

// GetMarketNews implements agent.MarketNewsProvider. Returns up to `limit` headlines sorted desc by
// publish time. symbols filters to those tickers (empty = market-wide). Unrecognized fields in the
// upstream response are ignored.
func (c *Client) GetMarketNews(ctx context.Context, symbols []string, limit int) ([]agent.MarketNewsItem, error) {
	if c == nil {
		return nil, fmt.Errorf("alpaca news: nil client")
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("sort", "desc")
	if syms := normalizeSymbols(symbols); len(syms) > 0 {
		q.Set("symbols", strings.Join(syms, ","))
	}
	endpoint := strings.TrimRight(c.cfg.DataBaseURL, "/") + defaultPath + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("alpaca news: build request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", c.cfg.KeyID)
	req.Header.Set("APCA-API-SECRET-KEY", c.cfg.SecretKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alpaca news: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("alpaca news: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("alpaca news: decode: %w", err)
	}
	out := make([]agent.MarketNewsItem, 0, len(parsed.News))
	for _, item := range parsed.News {
		headline := strings.TrimSpace(item.Headline)
		if headline == "" {
			continue
		}
		out = append(out, agent.MarketNewsItem{
			Title:     headline,
			Summary:   strings.TrimSpace(item.Summary),
			Source:    strings.TrimSpace(item.Source),
			Published: formatTime(item.CreatedAt),
			URL:       strings.TrimSpace(item.URL),
		})
	}
	return out, nil
}

// apiResponse mirrors the Alpaca news JSON envelope.
type apiResponse struct {
	News []apiNewsItem `json:"news"`
}

type apiNewsItem struct {
	ID        int64    `json:"id"`
	Headline  string   `json:"headline"`
	Summary   string   `json:"summary"`
	Author    string   `json:"author"`
	URL       string   `json:"url"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	Symbols   []string `json:"symbols"`
	Source    string   `json:"source"`
}

func normalizeSymbols(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func formatTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339)
	}
	return raw
}
