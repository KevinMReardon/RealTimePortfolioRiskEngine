package events

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

func TestEligibleBatchAfterWatermark_partialPageAppliesTailAtMaxSeen(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	ev := domain.EventEnvelope{EventID: uuid.New(), EventTime: t0}
	wm := 2 * time.Second
	cutoff := t0.Add(-wm) // max_seen - W; lone event at t0 is after cutoff
	batch := []domain.EventEnvelope{ev}

	got := eligibleBatchAfterWatermark(batch, cutoff, wm, fetchAfterPageLimit)
	if len(got) != 1 {
		t.Fatalf("want full partial batch applied, got len=%d", len(got))
	}
}

func TestEligibleBatchAfterWatermark_fullPageStillWaits(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	ev := domain.EventEnvelope{EventID: uuid.New(), EventTime: t0}
	wm := 2 * time.Second
	cutoff := t0.Add(-wm)
	batch := make([]domain.EventEnvelope, fetchAfterPageLimit)
	for i := range batch {
		batch[i] = ev
	}

	got := eligibleBatchAfterWatermark(batch, cutoff, wm, fetchAfterPageLimit)
	if len(got) != 0 {
		t.Fatalf("want empty prefix when page full and none eligible, got len=%d", len(got))
	}
}
