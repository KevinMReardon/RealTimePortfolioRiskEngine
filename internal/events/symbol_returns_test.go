package events

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestEventUTCDate(t *testing.T) {
	t.Parallel()

	in := time.Date(2026, 4, 7, 23, 59, 59, 0, time.FixedZone("UTC+2", 2*60*60))
	got := eventUTCDate(in)
	want := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("eventUTCDate got %v want %v", got, want)
	}
}

func TestComputeDailyReturn(t *testing.T) {
	t.Parallel()

	got, err := computeDailyReturn(decimal.NewFromInt(100), decimal.NewFromInt(110))
	if err != nil {
		t.Fatalf("computeDailyReturn err: %v", err)
	}
	want := decimal.RequireFromString("0.1")
	if !got.Equal(want) {
		t.Fatalf("daily return got %s want %s", got, want)
	}
}
