package news

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetMarketNews_ParsesResponse(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("APCA-API-KEY-ID") != "k" {
			t.Fatalf("expected api key header; got %q", r.Header.Get("APCA-API-KEY-ID"))
		}
		if r.URL.Query().Get("limit") == "" {
			t.Fatal("expected limit query param")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"news":[
				{"id":1,"headline":"Apple beats","summary":"earnings","author":"X","url":"https://x","created_at":"2024-01-02T15:04:05Z","symbols":["AAPL"],"source":"benzinga"},
				{"id":2,"headline":"","summary":"empty headline skipped"}
			]
		}`))
	}))
	defer ts.Close()
	cli := New(Config{DataBaseURL: ts.URL, KeyID: "k", SecretKey: "s"})
	items, err := cli.GetMarketNews(context.Background(), []string{"AAPL"}, 5)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 valid item (empty headline filtered); got %d", len(items))
	}
	if items[0].Title != "Apple beats" {
		t.Fatalf("title = %q", items[0].Title)
	}
	if items[0].Source != "benzinga" {
		t.Fatalf("source = %q", items[0].Source)
	}
	if items[0].URL != "https://x" {
		t.Fatalf("url = %q", items[0].URL)
	}
}

func TestGetMarketNews_NonOKStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer ts.Close()
	cli := New(Config{DataBaseURL: ts.URL, KeyID: "k", SecretKey: "s"})
	if _, err := cli.GetMarketNews(context.Background(), nil, 0); err == nil {
		t.Fatal("expected error on non-200 status")
	}
}
