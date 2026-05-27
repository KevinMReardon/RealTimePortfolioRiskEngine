// Package universe fetches the top liquid US equities from the Alpaca Market Data
// screener API and updates the price feed watchlist so the system tracks a rich
// symbol universe without manual configuration.
package universe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

const (
	// screenerPath is the Alpaca Market Data REST path for most-active equities.
	// See https://docs.alpaca.markets/reference/mostactives-1
	screenerPath = "/v1beta1/screener/stocks/most-actives"

	// defaultTop is the number of symbols requested when Top is unset.
	defaultTop = 100

	// maxTop is Alpaca's per-request cap for the screener "top" query param.
	maxTop = 100

	defaultHTTPTimeout = 15 * time.Second
)

// symbolRegexp mirrors domain.SymbolRegexp (avoids import cycle).
var symbolRegexp = regexp.MustCompile(`^[A-Z0-9._-]{1,32}$`)

// WatchlistPersistence persists the watchlist to durable storage (app_settings).
type WatchlistPersistence interface {
	UpsertPriceFeedWatchlist(ctx context.Context, symbols []string) error
}

// WatchlistRuntime optionally updates the live in-process price feed watchlist
// (satisfies ingestion/pricefeed.PriceIngestor via the api.PriceFeedWatchlistManager interface).
type WatchlistRuntime interface {
	SetWatchlist(symbols []string)
}

// WatchlistTickTrigger is implemented by the price feed ingestor to poll immediately
// after the watchlist changes.
type WatchlistTickTrigger interface {
	WatchlistRuntime
	TriggerTick(ctx context.Context)
}

// HydratorConfig configures the universe hydrator.
type HydratorConfig struct {
	// DataBaseURL is the Alpaca Market Data REST origin (e.g. https://data.alpaca.markets).
	DataBaseURL string
	// KeyID and SecretKey are the Alpaca API credentials.
	KeyID     string
	SecretKey string
	// Top is the number of most-active symbols to fetch (default 100, max 100 per Alpaca API).
	Top int
	// HTTPTimeout caps each screener HTTP request (default 15s).
	HTTPTimeout time.Duration
	// Log is used for structured info/warn logging; nil falls back to a nop logger.
	Log *zap.Logger
}

// Hydrator fetches top-liquid US equities daily and updates the price feed watchlist.
type Hydrator struct {
	cfg         HydratorConfig
	persistence WatchlistPersistence
	runtime     WatchlistRuntime // optional; nil = no live update
	httpClient  *http.Client
	log         *zap.Logger
}

// NewHydrator constructs a Hydrator. runtime may be nil when no live feed is running.
func NewHydrator(cfg HydratorConfig, persistence WatchlistPersistence, runtime WatchlistRuntime) *Hydrator {
	cfg.Top = clampTop(cfg.Top)
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	log := cfg.Log
	if log == nil {
		log = zap.NewNop()
	}
	return &Hydrator{
		cfg:         cfg,
		persistence: persistence,
		runtime:     runtime,
		httpClient:  &http.Client{Timeout: timeout},
		log:         log,
	}
}

// Run fetches the current top-N most-active symbols and stores them as the price
// feed watchlist. Existing watchlist is replaced entirely.
func (h *Hydrator) Run(ctx context.Context) error {
	symbols, err := h.fetchMostActives(ctx)
	if err != nil {
		h.log.Warn("universe_hydrator_fetch_failed", zap.Error(err))
		return fmt.Errorf("universe hydrator fetch: %w", err)
	}
	if len(symbols) == 0 {
		h.log.Warn("universe_hydrator_empty_response")
		return nil
	}
	if err := h.persistence.UpsertPriceFeedWatchlist(ctx, symbols); err != nil {
		h.log.Warn("universe_hydrator_persist_failed", zap.Error(err))
		return fmt.Errorf("universe hydrator persist: %w", err)
	}
	if h.runtime != nil {
		h.runtime.SetWatchlist(symbols)
		if tr, ok := h.runtime.(WatchlistTickTrigger); ok {
			tr.TriggerTick(ctx)
		}
	}
	h.log.Info("universe_hydrator_ok", zap.Int("symbols", len(symbols)))
	return nil
}

// mostActivesResponse is the Alpaca screener JSON shape.
type mostActivesResponse struct {
	MostActives []struct {
		Symbol string `json:"symbol"`
	} `json:"most_actives"`
}

func (h *Hydrator) fetchMostActives(ctx context.Context) ([]string, error) {
	base := strings.TrimRight(h.cfg.DataBaseURL, "/")
	url := fmt.Sprintf("%s%s?top=%d&by=volume", base, screenerPath, h.cfg.Top)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build screener request: %w", err)
	}
	req.Header.Set("APCA-API-KEY-ID", h.cfg.KeyID)
	req.Header.Set("APCA-API-SECRET-KEY", h.cfg.SecretKey)
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("screener http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("screener http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed mostActivesResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode screener response: %w", err)
	}

	out := make([]string, 0, len(parsed.MostActives))
	seen := make(map[string]struct{}, len(parsed.MostActives))
	for _, item := range parsed.MostActives {
		sym := strings.ToUpper(strings.TrimSpace(item.Symbol))
		if sym == "" {
			continue
		}
		if !symbolRegexp.MatchString(sym) {
			continue
		}
		if _, dup := seen[sym]; dup {
			continue
		}
		seen[sym] = struct{}{}
		out = append(out, sym)
	}
	return out, nil
}

func clampTop(top int) int {
	if top <= 0 {
		return defaultTop
	}
	if top > maxTop {
		return maxTop
	}
	return top
}

// RunLoop repeats Run on interval until ctx is done. Call Run once at startup before
// the price feed starts; this loop is for periodic refresh only.
func (h *Hydrator) RunLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.Run(ctx); err != nil {
				h.log.Warn("universe_hydrator_run_failed", zap.Error(err))
			}
		}
	}
}
