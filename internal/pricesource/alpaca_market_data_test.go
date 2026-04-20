package pricesource

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAlpacaSymbolMapping_RoundTrip(t *testing.T) {
	if got := internalToAlpacaEquitySymbol(" brk.b "); got != "BRK.B" {
		t.Fatalf("equity got %q", got)
	}
	if got := internalToAlpacaCryptoSymbol("BTC-USD"); got != "BTCUSD" {
		t.Fatalf("crypto got %q", got)
	}
	if !alpacaLooksLikeCryptoPair("ETH-USDT") {
		t.Fatal("expected crypto pair")
	}
	if alpacaLooksLikeCryptoPair("AAPL") {
		t.Fatal("equity should not be crypto")
	}
}

func TestAlpacaMarketDataProvider_FetchQuotes_multiEquity(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/v2/stocks/AAPL/trades/latest"):
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"symbol":"AAPL","trade":{"t":"2024-06-01T14:30:05.123456789Z","p":190.12,"s":100}}`))
		case strings.HasSuffix(r.URL.Path, "/v2/stocks/MSFT/trades/latest"):
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"symbol":"MSFT","trade":{"t":"2024-06-01T14:30:06Z","p":411,"s":10}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewAlpacaMarketDataProvider("k", "s", srv.URL, time.Second, 200)
	got, err := p.FetchQuotes(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Partial || len(got.Quotes) != 2 {
		t.Fatalf("partial=%v quotes=%d", got.Partial, len(got.Quotes))
	}
	h := p.Health()
	if !h.Healthy || h.Provider != "alpaca" {
		t.Fatalf("health %+v", h)
	}
}

func TestAlpacaMarketDataProvider_FetchQuotes_partialFailure(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/MSFT/trades/latest") {
			http.Error(w, "gone", http.StatusBadRequest)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"symbol":"AAPL","trade":{"t":"2024-06-01T14:30:05Z","p":100}}`))
	}))
	defer srv.Close()

	p := NewAlpacaMarketDataProvider("k", "s", srv.URL, time.Second, 200)
	got, err := p.FetchQuotes(context.Background(), []string{"AAPL", "MSFT"})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Partial || len(got.Quotes) != 1 {
		t.Fatalf("want partial one quote got partial=%v n=%d", got.Partial, len(got.Quotes))
	}
	if got.Quotes[0].Symbol != "AAPL" {
		t.Fatal(got.Quotes[0].Symbol)
	}
}

func TestAlpacaMarketDataProvider_FetchQuotes_requiresCredentials(t *testing.T) {
	t.Parallel()
	p := NewAlpacaMarketDataProvider("", "", "http://unused", time.Second, 200)
	_, err := p.FetchQuotes(context.Background(), []string{"AAPL"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAlpacaMarketDataProvider_FetchQuotes_cryptoBatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/v1beta3/crypto/us/latest/trades") {
			q := r.URL.Query().Get("symbols")
			if q == "" || !strings.Contains(q, "BTCUSD") {
				http.Error(w, "bad query", http.StatusBadRequest)
				return
			}
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"trades":{"BTCUSD":{"t":"2024-06-01T14:30:05Z","p":65000,"s":0.01}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	p := NewAlpacaMarketDataProvider("k", "s", srv.URL, time.Second, 200)
	got, err := p.FetchQuotes(context.Background(), []string{"BTC-USD"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Quotes) != 1 || got.Quotes[0].Symbol != "BTC-USD" {
		t.Fatalf("%+v", got.Quotes)
	}
}
