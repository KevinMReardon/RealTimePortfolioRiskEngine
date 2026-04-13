package events

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDebouncedRiskScheduler_CoalescesByPortfolio(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := map[uuid.UUID]int{}
	s := NewDebouncedRiskScheduler(25*time.Millisecond, func(pid uuid.UUID) {
		mu.Lock()
		calls[pid]++
		mu.Unlock()
	})
	defer s.Stop()

	pid := uuid.New()
	s.Schedule(pid)
	s.Schedule(pid)
	s.Schedule(pid)
	time.Sleep(80 * time.Millisecond)

	mu.Lock()
	got := calls[pid]
	mu.Unlock()
	if got != 1 {
		t.Fatalf("coalesced calls for one portfolio: got %d want 1", got)
	}
}

func TestDebouncedRiskScheduler_IsolatedPerPortfolio(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	calls := map[uuid.UUID]int{}
	s := NewDebouncedRiskScheduler(20*time.Millisecond, func(pid uuid.UUID) {
		mu.Lock()
		calls[pid]++
		mu.Unlock()
	})
	defer s.Stop()

	pidA := uuid.New()
	pidB := uuid.New()
	s.Schedule(pidA)
	s.Schedule(pidB)
	time.Sleep(70 * time.Millisecond)

	mu.Lock()
	a := calls[pidA]
	b := calls[pidB]
	mu.Unlock()
	if a != 1 || b != 1 {
		t.Fatalf("per-portfolio isolation got A=%d B=%d want A=1 B=1", a, b)
	}
}
