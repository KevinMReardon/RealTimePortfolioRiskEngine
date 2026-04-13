package events

import (
	"bytes"
	"time"

	"github.com/google/uuid"

	"github.com/KevinMReardon/realtime-portfolio-risk/internal/domain"
)

// Cursor is the deterministic ordering key (event_time ASC, event_id ASC) for incremental apply.
//
// Persistence is Option B (not in-memory-only Option A): table projection_cursor, migration
// 000003_projection_cursor.up.sql. The stored (last_event_time, last_event_id) is committed in
// the same database transaction as projection rows (ApplyBatch, ApplyPriceBatch, PersistApplyDLQ).
//
// Apply eligibility uses ORDERING_WATERMARK_MS as W: events with event_time <= max_seen - W,
// except a partial FetchAfter page applies in full so the tail at max_seen cannot deadlock
// (see trade_portfolio_apply.go eligibleBatchAfterWatermark).
type Cursor struct {
	Time time.Time
	ID   uuid.UUID
}

// CursorFromEvent returns the ordering cursor for an applied event.
func CursorFromEvent(ev domain.EventEnvelope) Cursor {
	return Cursor{Time: ev.EventTime, ID: ev.EventID}
}

// IsZero reports whether the cursor is unset (read from the start of the stream).
// For persistence, an uninitialized portfolio has no row in projection_cursor; LoadProjectionCursor
// returns Cursor{} which satisfies IsZero. We do not store a sentinel row for “empty”.
func (c Cursor) IsZero() bool {
	return c.ID == uuid.Nil && c.Time.IsZero()
}

// CompareCursors compares a and b in apply order (event_time ASC, event_id ASC).
// Returns -1 if a is before b, 0 if equal, 1 if a is after b.
func CompareCursors(a, b Cursor) int {
	if a.Time.Before(b.Time) {
		return -1
	}
	if a.Time.After(b.Time) {
		return 1
	}
	return bytes.Compare(a.ID[:], b.ID[:])
}
