package pricefeed

import (
	"sync"
	"time"
)

// RuntimeTracker records the latest automated price feed tick outcomes for read APIs.
type RuntimeTracker struct {
	mu sync.RWMutex

	lastTickStartedAt *time.Time
	lastTickFinishedAt *time.Time
	lastSuccessAt        *time.Time
	lastError            string
	activeProvider       string
	lastTickUsedFailover bool
	lastTickIngested     int
}

func NewRuntimeTracker() *RuntimeTracker {
	return &RuntimeTracker{}
}

func (t *RuntimeTracker) OnTickStart(at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	at = at.UTC()
	t.lastTickStartedAt = &at
}

func (t *RuntimeTracker) OnTickFailure(at time.Time, err error) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	at = at.UTC()
	t.lastTickFinishedAt = &at
	if err != nil {
		t.lastError = err.Error()
	}
	t.lastTickIngested = 0
}

func (t *RuntimeTracker) OnTickSuccess(at time.Time, provider string, usedFailover bool, ingested int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	at = at.UTC()
	t.lastTickFinishedAt = &at
	t.lastSuccessAt = &at
	t.lastError = ""
	t.activeProvider = provider
	t.lastTickUsedFailover = usedFailover
	t.lastTickIngested = ingested
}

// Snapshot returns the latest runtime fields (safe for concurrent reads).
func (t *RuntimeTracker) Snapshot() RuntimeSnapshot {
	if t == nil {
		return RuntimeSnapshot{}
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return RuntimeSnapshot{
		LastTickStartedAt:    copyTimePtr(t.lastTickStartedAt),
		LastTickFinishedAt:   copyTimePtr(t.lastTickFinishedAt),
		LastSuccessAt:        copyTimePtr(t.lastSuccessAt),
		LastError:            t.lastError,
		ActiveProvider:       t.activeProvider,
		LastTickUsedFailover: t.lastTickUsedFailover,
		LastTickIngested:     t.lastTickIngested,
	}
}

type RuntimeSnapshot struct {
	LastTickStartedAt    *time.Time
	LastTickFinishedAt   *time.Time
	LastSuccessAt        *time.Time
	LastError            string
	ActiveProvider       string
	LastTickUsedFailover bool
	LastTickIngested     int
}

func copyTimePtr(p *time.Time) *time.Time {
	if p == nil {
		return nil
	}
	v := p.UTC()
	return &v
}
