DROP INDEX IF EXISTS idx_portfolio_snapshots_portfolio_as_of_latest;

CREATE INDEX IF NOT EXISTS idx_portfolio_snapshots_portfolio_as_of
    ON portfolio_snapshots (portfolio_id, as_of_event_time DESC);
