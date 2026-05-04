package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestPricePayload_MarkAsOfTime(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 5, 4, 16, 0, 0, 0, time.UTC)
	fallback := time.Date(2026, 5, 4, 19, 0, 0, 0, time.UTC)

	p := PricePayload{
		Symbol: "AMZN",
		Price:  decimal.RequireFromString("100"),
		AsOf:   ts,
	}
	if got := p.MarkAsOfTime(fallback); !got.Equal(ts) {
		t.Fatalf("MarkAsOfTime got %v want %v", got, ts)
	}

	p2 := PricePayload{Symbol: "AMZN", Price: decimal.RequireFromString("100")}
	if got := p2.MarkAsOfTime(fallback); !got.Equal(fallback) {
		t.Fatalf("MarkAsOfTime fallback got %v want %v", got, fallback)
	}
}
