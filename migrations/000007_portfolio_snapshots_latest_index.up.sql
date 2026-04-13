-- Latest checkpoint per portfolio is defined by apply cursor (event_time, event_id), not
-- insertion order. Replace two-column index with full tuple so ORDER BY
-- as_of_event_time DESC, as_of_event_id DESC LIMIT 1 uses the index end-to-end.
DROP INDEX IF EXISTS idx_portfolio_snapshots_portfolio_as_of;

CREATE INDEX IF NOT EXISTS idx_portfolio_snapshots_portfolio_as_of_latest
    ON portfolio_snapshots (portfolio_id, as_of_event_time DESC, as_of_event_id DESC);
