package bars

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetDailyBars_Parses(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("timeframe") != "1Day" {
			t.Fatalf("timeframe = %q", r.URL.Query().Get("timeframe"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bars":[
			{"t":"2024-01-02T05:00:00Z","o":100.0,"h":101.0,"l":99.5,"c":100.5,"v":12345},
			{"t":"2024-01-03T05:00:00Z","o":100.5,"h":102.0,"l":100.0,"c":101.5,"v":23456}
		]}`))
	}))
	defer ts.Close()
	cli := New(Config{DataBaseURL: ts.URL, KeyID: "k", SecretKey: "s"})
	rows, err := cli.GetDailyBars(context.Background(), "aapl", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].C != 100.5 || rows[1].C != 101.5 {
		t.Fatalf("closes = %v, %v", rows[0].C, rows[1].C)
	}
}

func TestGetDailyBars_EmptySymbol(t *testing.T) {
	cli := New(Config{KeyID: "k", SecretKey: "s"})
	if _, err := cli.GetDailyBars(context.Background(), "  ", 10); err == nil {
		t.Fatal("expected error on empty symbol")
	}
}
