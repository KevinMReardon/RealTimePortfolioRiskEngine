-- One logical checkpoint per (portfolio_id, as_of_event_time, as_of_event_id) for idempotent
-- snapshot writes (ON CONFLICT DO NOTHING). Replaces the non-unique "latest" index: this btree
-- still supports backward scans for ORDER BY as_of_event_time DESC, as_of_event_id DESC per portfolio.
DROP INDEX IF EXISTS idx_portfolio_snapshots_portfolio_as_of_latest;

CREATE UNIQUE INDEX IF NOT EXISTS uq_portfolio_snapshots_checkpoint
    ON portfolio_snapshots (portfolio_id, as_of_event_time, as_of_event_id);
