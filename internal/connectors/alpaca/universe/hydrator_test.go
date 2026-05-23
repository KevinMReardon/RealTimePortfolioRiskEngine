package universe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockPersistence struct {
	saved []string
}

func (m *mockPersistence) UpsertPriceFeedWatchlist(_ context.Context, symbols []string) error {
	m.saved = symbols
	return nil
}

type mockRuntime struct {
	set []string
}

func (m *mockRuntime) SetWatchlist(symbols []string) {
	m.set = symbols
}

func TestHydrator_Run_FetchesAndPersists(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/screener/stocks/most-actives" {
			http.NotFound(w, r)
			return
		}
		resp := map[string]any{
			"most_actives": []map[string]any{
				{"symbol": "AAPL", "volume": 1000},
				{"symbol": "MSFT", "volume": 900},
				{"symbol": "NVDA", "volume": 800},
				{"symbol": "bad symbol!", "volume": 1},  // should be filtered
				{"symbol": "", "volume": 0},              // empty, filtered
				{"symbol": "AAPL", "volume": 500},        // duplicate, filtered
			},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	p := &mockPersistence{}
	rt := &mockRuntime{}
	h := NewHydrator(HydratorConfig{
		DataBaseURL: srv.URL,
		KeyID:       "test-key",
		SecretKey:   "test-secret",
		Top:         10,
	}, p, rt)

	if err := h.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"AAPL", "MSFT", "NVDA"}
	if len(p.saved) != len(want) {
		t.Fatalf("saved %d symbols, want %d: %v", len(p.saved), len(want), p.saved)
	}
	for i, sym := range want {
		if p.saved[i] != sym {
			t.Errorf("saved[%d] = %q, want %q", i, p.saved[i], sym)
		}
	}
	if len(rt.set) != len(want) {
		t.Fatalf("runtime set %d symbols, want %d", len(rt.set), len(want))
	}
}

func TestHydrator_Run_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := &mockPersistence{}
	h := NewHydrator(HydratorConfig{
		DataBaseURL: srv.URL,
		KeyID:       "bad",
		SecretKey:   "bad",
	}, p, nil)

	err := h.Run(context.Background())
	if err == nil {
		t.Fatal("expected error on HTTP 401, got nil")
	}
	if len(p.saved) != 0 {
		t.Fatalf("expected no persistence on error, got %v", p.saved)
	}
}

func TestHydrator_Run_EmptyResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"most_actives":[]}`))
	}))
	defer srv.Close()

	p := &mockPersistence{}
	h := NewHydrator(HydratorConfig{
		DataBaseURL: srv.URL,
		KeyID:       "k",
		SecretKey:   "s",
	}, p, nil)

	if err := h.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(p.saved) != 0 {
		t.Fatalf("expected no persistence for empty response, got %v", p.saved)
	}
}
