package ai

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExplain_endToEnd_mockHTTP(t *testing.T) {
	t.Parallel()
	body := `{"choices":[{"message":{"content":"{\"narrative\":\"AAPL risk is in context.\",\"used_metrics\":[\"risk.var_95_1d\"]}"}}]}`
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || !strings.HasSuffix(req.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
		}
		b, _ := io.ReadAll(req.Body)
		if !strings.Contains(string(b), `"model":"gpt-test"`) {
			t.Fatalf("request body missing model: %s", string(b))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})
	client := &http.Client{Transport: rt}
	ctx := context.Background()
	in := ExplainInput{
		ContextJSON: []byte(`{"portfolio":{"positions":[{"symbol":"AAPL"}]},"risk":{"var_95_1d":"1","exposure":[{"symbol":"AAPL"}]},"recent_events":[]}`),
		ClientPayload: []byte(`{}`),
		AllowSymbols:  map[string]struct{}{"AAPL": {}},
		Model:         "gpt-test",
		BaseURL:       "https://example.test/v1",
	}
	out, err := Explain(ctx, client, "sk-test", in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Explanation != "AAPL risk is in context." {
		t.Fatalf("explanation = %q", out.Explanation)
	}
	if len(out.UsedMetrics) != 1 || out.UsedMetrics[0] != "risk.var_95_1d" {
		t.Fatalf("used_metrics = %#v", out.UsedMetrics)
	}
}

func TestExplain_serverFillsUsedMetricsWhenModelOmits(t *testing.T) {
	t.Parallel()
	body := `{"choices":[{"message":{"content":"{\"narrative\":\"AAPL is in context.\",\"used_metrics\":[]}"}}]}`
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})
	client := &http.Client{Transport: rt}
	ctx := context.Background()
	in := ExplainInput{
		ContextJSON: []byte(`{
  "portfolio":{"totals":{"market_value":"100","unrealized_pnl":"0","realized_pnl":"0"}},
  "risk":{"var_95_1d":"1","volatility":{"sigma_1d_portfolio":"0.1","by_symbol":[]},"exposure":[{"symbol":"AAPL"}]},
  "recent_events":[]
}`),
		ClientPayload: []byte(`{}`),
		AllowSymbols:  map[string]struct{}{"AAPL": {}},
		Model:         "gpt-test",
		BaseURL:       "https://example.test/v1",
	}
	out, err := Explain(ctx, client, "sk-test", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.UsedMetrics) == 0 {
		t.Fatal("expected server-filled used_metrics")
	}
	if out.Model != "gpt-test" {
		t.Fatalf("model = %q", out.Model)
	}
}

func TestExplain_sanitizesUnknownTickerSymbol(t *testing.T) {
	t.Parallel()
	body := `{"choices":[{"message":{"content":"{\"narrative\":\"AAPL outperformed HHI.\",\"used_metrics\":[\"risk.exposure\"]}"}}]}`
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})
	client := &http.Client{Transport: rt}
	ctx := context.Background()
	in := ExplainInput{
		ContextJSON:   []byte(`{"portfolio":{"positions":[{"symbol":"AAPL"}]},"risk":{"exposure":[{"symbol":"AAPL"}]},"recent_events":[]}`),
		ClientPayload: []byte(`{}`),
		AllowSymbols:  map[string]struct{}{"AAPL": {}},
		Model:         "gpt-test",
		BaseURL:       "https://example.test/v1",
	}
	out, err := Explain(ctx, client, "sk-test", in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Explanation, "HHI") {
		t.Fatalf("unknown ticker should be sanitized: %q", out.Explanation)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
