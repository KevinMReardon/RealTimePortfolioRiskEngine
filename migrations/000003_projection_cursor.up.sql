-- Apply cursor: Option A vs Option B
--
-- Option A — Cursor only in process memory: fast, but after restart you must replay from
--   the beginning of the event stream (or from a snapshot) unless something else tracks progress.
--
-- Option B — Cursor persisted in this table: the last applied ordering key (last_event_time,
--   last_event_id) is written in the SAME database transaction as the materialized projection
--   rows (e.g. positions_projection / prices_projection in ApplyBatch / ApplyPriceBatch, and
--   DLQ+cursor advances). This repo uses Option B.
--
-- Ordering buffer (application config, not this migration): ORDERING_WATERMARK_MS defines W.
--   For each portfolio partition, max_seen = MAX(event_time) over all events; only events with
--   event_time <= max_seen - W are eligible to apply yet (reduces applying the very newest row
--   before slightly-older rows are visible). See internal/events/trade_portfolio_apply.go (pumpAfterCursor).
--
-- Semantics:
--   - One row per portfolio that has applied at least one event.
--   - Empty / never-applied portfolio: no row for that portfolio_id. The app maps this to
--     Cursor{}.IsZero() in Go (uuid.Nil + zero time); see internal/events/cursor.go and
--     LoadProjectionCursor.
--   - Do not rely on placeholder rows (e.g. nil UUID) — absence of a row is the source of truth.

CREATE TABLE IF NOT EXISTS projection_cursor (
    portfolio_id UUID PRIMARY KEY,
    last_event_time TIMESTAMPTZ NOT NULL,
    last_event_id UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
