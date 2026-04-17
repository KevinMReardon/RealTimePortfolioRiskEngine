package pricesource

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestTwelveDataProviderFetchQuotes_MultiSymbol(t *testing.T) {
	t.Parallel()

	var gotSymbols string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSymbols = r.URL.Query().Get("symbol")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"AAPL": {"price":"199.12","currency":"USD"},
			"BTC/USD": {"price":"60123.10","currency":"USD"},
			"EUR/USD": {"price":"1.0812","currency":"USD"}
		}`))
	}))
	defer srv.Close()

	p := NewTwelveDataProvider("key", 2*time.Second, 8)
	p.baseURL = srv.URL

	res, err := p.FetchQuotes(t.Context(), []string{"AAPL", "BTC-USD", "EUR-USD"})
	if err != nil {
		t.Fatalf("FetchQuotes error: %v", err)
	}
	if gotSymbols == "" {
		t.Fatal("expected symbol query param")
	}
	decoded, err := url.QueryUnescape(gotSymbols)
	if err != nil {
		t.Fatalf("decode symbol param: %v", err)
	}
	if decoded != "AAPL,BTC/USD,EUR/USD" {
		t.Fatalf("symbol request got %q want %q", decoded, "AAPL,BTC/USD,EUR/USD")
	}
	if len(res.Quotes) != 3 {
		t.Fatalf("quotes length got %d want 3", len(res.Quotes))
	}
	if res.Quotes[0].Symbol == "" || res.Quotes[1].Symbol == "" || res.Quotes[2].Symbol == "" {
		t.Fatalf("normalized symbols should not be empty: %+v", res.Quotes)
	}
	if res.Quotes[1].Symbol != "BTC-USD" {
		t.Fatalf("symbol normalization got %q want BTC-USD", res.Quotes[1].Symbol)
	}
	if res.Quotes[2].Symbol != "EUR-USD" {
		t.Fatalf("symbol normalization got %q want EUR-USD", res.Quotes[2].Symbol)
	}
	h := p.Health()
	if !h.Healthy {
		t.Fatalf("expected healthy provider, got %+v", h)
	}
}
