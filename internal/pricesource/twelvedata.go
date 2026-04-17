package pricesource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

const defaultTwelveDataBaseURL = "https://api.twelvedata.com"

type TwelveDataProvider struct {
	apiKey       string
	baseURL      string
	rateLimitRPM int
	client       *http.Client

	mu     sync.RWMutex
	health HealthMetadata
}

func NewTwelveDataProvider(apiKey string, timeout time.Duration, rateLimitRPM int) *TwelveDataProvider {
	base := defaultTwelveDataBaseURL
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &TwelveDataProvider{
		apiKey:       strings.TrimSpace(apiKey),
		baseURL:      base,
		rateLimitRPM: rateLimitRPM,
		client:       &http.Client{Timeout: timeout},
		health: HealthMetadata{
			Provider:     "twelvedata",
			Healthy:      false,
			RateLimitRPM: rateLimitRPM,
		},
	}
}

func (p *TwelveDataProvider) Name() string {
	return "twelvedata"
}

func (p *TwelveDataProvider) Health() HealthMetadata {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.health
}

func (p *TwelveDataProvider) FetchQuotes(ctx context.Context, symbols []string) (FetchResult, error) {
	start := time.Now()
	clean := normalizeRequestedSymbols(symbols)
	if len(clean) == 0 {
		err := fmt.Errorf("symbols are required")
		p.updateHealth(start, 0, nil, err)
		return FetchResult{}, err
	}

	requestSymbols := buildTwelveDataRequestSymbols(clean)
	query := url.Values{}
	query.Set("symbol", strings.Join(requestSymbols.providerList(), ","))
	query.Set("apikey", p.apiKey)
	u := p.baseURL + "/price?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		p.updateHealth(start, len(clean), nil, err)
		return FetchResult{}, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		wrapped := ProviderError{Provider: p.Name(), Retryable: true, Err: err}
		p.updateHealth(start, len(clean), nil, wrapped)
		return FetchResult{}, wrapped
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		wrapped := ProviderError{
			Provider:   p.Name(),
			StatusCode: resp.StatusCode,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			Err:        fmt.Errorf("unexpected status"),
		}
		p.updateHealth(start, len(clean), nil, wrapped)
		return FetchResult{}, wrapped
	}

	quotes, parseErr := parseTwelveDataPriceResponse(resp, requestSymbols)
	if parseErr != nil {
		p.updateHealth(start, len(clean), nil, parseErr)
		return FetchResult{}, parseErr
	}
	res := FetchResult{
		Provider:  p.Name(),
		FetchedAt: time.Now().UTC(),
		Quotes:    quotes,
		Partial:   len(quotes) != len(clean),
	}
	p.updateHealth(start, len(clean), &res, nil)
	return res, nil
}

type twelveDataRequestSymbols struct {
	orderedInternal []string
	internalToAPI   map[string]string
	apiToInternal   map[string]string
}

func buildTwelveDataRequestSymbols(internalSymbols []string) twelveDataRequestSymbols {
	out := twelveDataRequestSymbols{
		orderedInternal: make([]string, 0, len(internalSymbols)),
		internalToAPI:   make(map[string]string, len(internalSymbols)),
		apiToInternal:   make(map[string]string, len(internalSymbols)),
	}
	for _, internal := range internalSymbols {
		apiSymbol := toTwelveDataSymbol(internal)
		out.orderedInternal = append(out.orderedInternal, internal)
		out.internalToAPI[internal] = apiSymbol
		out.apiToInternal[apiSymbol] = internal
	}
	return out
}

func (s twelveDataRequestSymbols) providerList() []string {
	out := make([]string, 0, len(s.orderedInternal))
	for _, internal := range s.orderedInternal {
		out = append(out, s.internalToAPI[internal])
	}
	return out
}

func (p *TwelveDataProvider) updateHealth(start time.Time, requested int, res *FetchResult, err error) {
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

func parseTwelveDataPriceResponse(resp *http.Response, requested twelveDataRequestSymbols) ([]PriceQuote, error) {
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode twelvedata response: %w", err)
	}

	// Error response shape usually includes code + message.
	if rawCode, ok := body["code"]; ok && rawCode != nil {
		return nil, fmt.Errorf("twelvedata error response")
	}

	quotes := make([]PriceQuote, 0, len(requested.orderedInternal))
	now := time.Now().UTC()

	// Single-symbol shape: {"price":"123.45"}
	if len(requested.orderedInternal) == 1 {
		if raw, ok := body["price"]; ok {
			price, err := parsePriceString(raw)
			if err != nil {
				return nil, err
			}
			internal := requested.orderedInternal[0]
			providerSymbol := requested.internalToAPI[internal]
			quotes = append(quotes, PriceQuote{
				Symbol:         internal,
				ProviderSymbol: providerSymbol,
				Price:          price,
				Currency:       "USD",
				AsOf:           now,
				SourceSequence: now.UnixMilli(),
			})
			return quotes, nil
		}
	}

	// Multi-symbol shape: {"AAPL":{"price":"..."},"MSFT":{"price":"..."}}
	for _, reqInternal := range requested.orderedInternal {
		reqSym := requested.internalToAPI[reqInternal]
		raw, ok := body[reqSym]
		if !ok {
			continue
		}
		var row struct {
			Price    string `json:"price"`
			Currency string `json:"currency"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			continue
		}
		if strings.TrimSpace(row.Price) == "" {
			continue
		}
		price, err := decimal.NewFromString(strings.TrimSpace(row.Price))
		if err != nil {
			continue
		}
		currency := strings.ToUpper(strings.TrimSpace(row.Currency))
		if currency == "" {
			currency = "USD"
		}
		quotes = append(quotes, PriceQuote{
			Symbol:         reqInternal,
			ProviderSymbol: reqSym,
			Price:          price,
			Currency:       currency,
			AsOf:           now,
			SourceSequence: now.UnixMilli(),
		})
	}
	if len(quotes) == 0 {
		return nil, fmt.Errorf("twelvedata returned no parseable quotes")
	}
	return quotes, nil
}

func parsePriceString(raw json.RawMessage) (decimal.Decimal, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return decimal.Zero, fmt.Errorf("decode twelvedata price: %w", err)
	}
	p, err := decimal.NewFromString(strings.TrimSpace(s))
	if err != nil {
		return decimal.Zero, fmt.Errorf("parse twelvedata price: %w", err)
	}
	return p, nil
}

func normalizeRequestedSymbols(symbols []string) []string {
	out := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, s := range symbols {
		v := strings.TrimSpace(s)
		if v == "" {
			continue
		}
		u := strings.ToUpper(v)
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	return out
}

func toTwelveDataSymbol(internal string) string {
	s := strings.ToUpper(strings.TrimSpace(internal))
	if s == "" {
		return s
	}
	parts := strings.Split(s, "-")
	if len(parts) == 2 && len(parts[0]) >= 3 && len(parts[1]) >= 3 {
		return parts[0] + "/" + parts[1]
	}
	return s
}
