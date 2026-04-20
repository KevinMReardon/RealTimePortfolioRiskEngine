package pricesource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// DefaultAlpacaMarketDataURL is Alpaca Market Data REST (distinct from trading REST).
const DefaultAlpacaMarketDataURL = "https://data.alpaca.markets"

// AlpacaMarketDataProvider fetches latest trades via Alpaca Market Data HTTP API (polling path).
// Equities use GET /v2/stocks/{symbol}/trades/latest; crypto pairs (internal BASE-QUOTE) use
// GET /v1beta3/crypto/us/latest/trades?symbols=BTCUSD,… — see mapping helpers below.
type AlpacaMarketDataProvider struct {
	keyID       string
	secretKey   string
	baseURL     string
	rateLimitRPM int
	client      *http.Client

	mu     sync.RWMutex
	health HealthMetadata
}

func NewAlpacaMarketDataProvider(keyID, secretKey, dataBaseURL string, timeout time.Duration, rateLimitRPM int) *AlpacaMarketDataProvider {
	base := strings.TrimSuffix(strings.TrimSpace(dataBaseURL), "/")
	if base == "" {
		base = strings.TrimSuffix(DefaultAlpacaMarketDataURL, "/")
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	if rateLimitRPM < 1 {
		rateLimitRPM = 200
	}
	return &AlpacaMarketDataProvider{
		keyID:       strings.TrimSpace(keyID),
		secretKey:   strings.TrimSpace(secretKey),
		baseURL:     base,
		rateLimitRPM: rateLimitRPM,
		client:      &http.Client{Timeout: timeout},
		health: HealthMetadata{
			Provider:     "alpaca",
			Healthy:      false,
			RateLimitRPM: rateLimitRPM,
		},
	}
}

func (p *AlpacaMarketDataProvider) Name() string {
	return "alpaca"
}

func (p *AlpacaMarketDataProvider) Health() HealthMetadata {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

func (p *AlpacaMarketDataProvider) FetchQuotes(ctx context.Context, symbols []string) (FetchResult, error) {
	start := time.Now()
	clean := normalizeRequestedSymbols(symbols)
	if len(clean) == 0 {
		err := fmt.Errorf("symbols are required")
		p.updateHealth(start, 0, nil, err)
		return FetchResult{}, err
	}
	if p.keyID == "" || p.secretKey == "" {
		err := fmt.Errorf("alpaca market data: credentials required")
		p.updateHealth(start, len(clean), nil, err)
		return FetchResult{}, err
	}

	var stocks, crypto []string
	for _, s := range clean {
		if alpacaLooksLikeCryptoPair(s) {
			crypto = append(crypto, s)
		} else {
			stocks = append(stocks, s)
		}
	}

	var quotes []PriceQuote
	var errs []error

	for _, internal := range stocks {
		apiSym := internalToAlpacaEquitySymbol(internal)
		q, err := p.fetchStockLatestTrade(ctx, internal, apiSym)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		quotes = append(quotes, q)
	}

	if len(crypto) > 0 {
		cq, cerr := p.fetchCryptoLatestTrades(ctx, crypto)
		if cerr != nil {
			errs = append(errs, cerr)
		}
		quotes = append(quotes, cq...)
	}

	partial := len(quotes) != len(clean)
	if len(quotes) == 0 {
		err := fmt.Errorf("alpaca: no quotes (%d symbol errors)", len(errs))
		if len(errs) > 0 {
			err = fmt.Errorf("alpaca: %w; first: %v", err, errs[0])
		}
		p.updateHealth(start, len(clean), nil, err)
		return FetchResult{}, err
	}

	res := FetchResult{
		Provider:  p.Name(),
		FetchedAt: time.Now().UTC(),
		Quotes:    quotes,
		Partial:   partial,
	}
	p.updateHealth(start, len(clean), &res, nil)
	return res, nil
}

func (p *AlpacaMarketDataProvider) authHeaders() http.Header {
	h := make(http.Header)
	h.Set("Accept", "application/json")
	h.Set("APCA-API-KEY-ID", p.keyID)
	h.Set("APCA-API-SECRET-KEY", p.secretKey)
	return h
}

type alpacaStockLatestTradeResp struct {
	Symbol string `json:"symbol"`
	Trade  struct {
		T string          `json:"t"`
		P json.RawMessage `json:"p"`
	} `json:"trade"`
}

func (p *AlpacaMarketDataProvider) fetchStockLatestTrade(ctx context.Context, internalSymbol, apiPathSymbol string) (PriceQuote, error) {
	u := fmt.Sprintf("%s/v2/stocks/%s/trades/latest", p.baseURL, url.PathEscape(apiPathSymbol))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return PriceQuote{}, err
	}
	req.Header = p.authHeaders()

	resp, err := p.client.Do(req)
	if err != nil {
		return PriceQuote{}, ProviderError{Provider: p.Name(), Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return PriceQuote{}, ProviderError{
			Provider:   p.Name(),
			StatusCode: resp.StatusCode,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			Err:        fmt.Errorf("stock latest trade %s: status %d: %s", internalSymbol, resp.StatusCode, truncateBody(body)),
		}
	}

	var parsed alpacaStockLatestTradeResp
	if err := json.Unmarshal(body, &parsed); err != nil {
		return PriceQuote{}, fmt.Errorf("decode stock trade %s: %w", internalSymbol, err)
	}
	price, err := parseAlpacaPriceJSON(parsed.Trade.P)
	if err != nil {
		return PriceQuote{}, fmt.Errorf("parse trade price %s: %w", internalSymbol, err)
	}
	asOf, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(parsed.Trade.T))
	if err != nil {
		asOf = time.Now().UTC()
	} else {
		asOf = asOf.UTC()
	}

	return PriceQuote{
		Symbol:         internalSymbol,
		ProviderSymbol: apiPathSymbol,
		Price:          price,
		Currency:       "USD",
		AsOf:           asOf,
		SourceSequence: asOf.UnixMilli(),
	}, nil
}

func parseAlpacaPriceJSON(raw json.RawMessage) (decimal.Decimal, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return decimal.Zero, fmt.Errorf("empty price")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return decimal.Zero, err
		}
		return decimal.NewFromString(strings.TrimSpace(s))
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromFloat(f), nil
}

func truncateBody(b []byte) string {
	const max = 200
	s := string(b)
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// crypto latest trades: /v1beta3/crypto/us/latest/trades?symbols=BTCUSD,ETHUSD
func (p *AlpacaMarketDataProvider) fetchCryptoLatestTrades(ctx context.Context, internalSymbols []string) ([]PriceQuote, error) {
	if len(internalSymbols) == 0 {
		return nil, nil
	}
	apiToInternal := make(map[string]string)
	var apiList []string
	for _, internal := range internalSymbols {
		api := internalToAlpacaCryptoSymbol(internal)
		if api == "" {
			continue
		}
		if _, dup := apiToInternal[api]; dup {
			continue
		}
		apiToInternal[api] = internal
		apiList = append(apiList, api)
	}
	if len(apiList) == 0 {
		return nil, fmt.Errorf("no mappable crypto symbols")
	}

	q := url.Values{}
	q.Set("symbols", strings.Join(apiList, ","))
	u := p.baseURL + "/v1beta3/crypto/us/latest/trades?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header = p.authHeaders()

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, ProviderError{Provider: p.Name(), Retryable: true, Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ProviderError{
			Provider:   p.Name(),
			StatusCode: resp.StatusCode,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			Err:        fmt.Errorf("crypto latest trades: status %d: %s", resp.StatusCode, truncateBody(body)),
		}
	}

	var parsed struct {
		Trades map[string]json.RawMessage `json:"trades"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode crypto trades: %w", err)
	}
	if len(parsed.Trades) == 0 {
		return nil, fmt.Errorf("crypto: empty trades map")
	}

	out := make([]PriceQuote, 0, len(parsed.Trades))
	for apiSym, raw := range parsed.Trades {
		internal, ok := apiToInternal[apiSym]
		if !ok {
			continue
		}
		var trade struct {
			T string          `json:"t"`
			P json.RawMessage `json:"p"`
		}
		if err := json.Unmarshal(raw, &trade); err != nil {
			continue
		}
		price, err := parseAlpacaPriceJSON(trade.P)
		if err != nil {
			continue
		}
		asOf, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(trade.T))
		if err != nil {
			asOf = time.Now().UTC()
		} else {
			asOf = asOf.UTC()
		}
		out = append(out, PriceQuote{
			Symbol:         internal,
			ProviderSymbol: apiSym,
			Price:          price,
			Currency:       "USD",
			AsOf:           asOf,
			SourceSequence: asOf.UnixMilli(),
		})
	}
	return out, nil
}

func (p *AlpacaMarketDataProvider) updateHealth(start time.Time, requested int, res *FetchResult, err error) {
	now := time.Now().UTC()
	h := HealthMetadata{
		Provider:         p.Name(),
		CheckedAt:        now,
		LastFetchLatency: time.Since(start),
		RateLimitRPM:     p.rateLimitRPM,
		LastRequestCount: requested,
	}
	if err != nil {
		h.Healthy = false
		h.LastError = err.Error()
	} else {
		h.Healthy = true
		if res != nil && len(res.Quotes) > 0 {
			ts := now
			h.LastSuccessAt = &ts
		}
	}
	p.mu.Lock()
	p.health = h
	p.mu.Unlock()
}

// alpacaLooksLikeCryptoPair treats internal symbols with a single hyphen separating
// base and quote (e.g. BTC-USD, ETH-USDT) as Alpaca crypto pairs. US equities use
// dots (BRK.B) or plain tickers (AAPL) without this pattern.
func alpacaLooksLikeCryptoPair(internal string) bool {
	s := strings.ToUpper(strings.TrimSpace(internal))
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return false
	}
	base, quote := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if len(base) < 2 || len(quote) < 2 {
		return false
	}
	switch quote {
	case "USD", "USDT", "EUR", "GBP":
		return true
	default:
		return false
	}
}

// internalToAlpacaEquitySymbol maps internal canonical symbols to Alpaca stock symbols (usually identical).
func internalToAlpacaEquitySymbol(internal string) string {
	return strings.ToUpper(strings.TrimSpace(internal))
}

// internalToAlpacaCryptoSymbol maps internal BASE-QUOTE (e.g. BTC-USD) to Alpaca crypto feed symbols (BTCUSD).
func internalToAlpacaCryptoSymbol(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return ""
	}
	b, q := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if b == "" || q == "" {
		return ""
	}
	return b + q
}
