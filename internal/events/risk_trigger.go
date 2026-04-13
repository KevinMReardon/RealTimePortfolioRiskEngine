package events

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// RiskRecomputeScheduler debounces per-portfolio risk recompute triggers.
type RiskRecomputeScheduler interface {
	Schedule(portfolioID uuid.UUID)
	Stop()
}

// DebouncedRiskScheduler coalesces bursts into one callback per portfolio after delay.
type DebouncedRiskScheduler struct {
	mu      sync.Mutex
	delay   time.Duration
	timers  map[uuid.UUID]*time.Timer
	trigger func(portfolioID uuid.UUID)
}

// NewDebouncedRiskScheduler creates a per-portfolio debounce scheduler.
func NewDebouncedRiskScheduler(delay time.Duration, trigger func(portfolioID uuid.UUID)) *DebouncedRiskScheduler {
	if delay <= 0 {
		delay = 250 * time.Millisecond
	}
	return &DebouncedRiskScheduler{
		delay:   delay,
		timers:  make(map[uuid.UUID]*time.Timer),
		trigger: trigger,
	}
}

// Schedule resets (or creates) the debounce timer for portfolioID.
func (s *DebouncedRiskScheduler) Schedule(portfolioID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.timers[portfolioID]; ok {
		t.Stop()
	}
	pid := portfolioID
	s.timers[portfolioID] = time.AfterFunc(s.delay, func() {
		if s.trigger != nil {
			s.trigger(pid)
		}
		s.mu.Lock()
		delete(s.timers, pid)
		s.mu.Unlock()
	})
}

// Stop cancels all outstanding timers.
func (s *DebouncedRiskScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.timers {
		t.Stop()
	}
	s.timers = make(map[uuid.UUID]*time.Timer)
}
