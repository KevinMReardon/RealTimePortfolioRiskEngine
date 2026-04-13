DROP INDEX IF EXISTS uq_portfolio_snapshots_checkpoint;

CREATE INDEX IF NOT EXISTS idx_portfolio_snapshots_portfolio_as_of_latest
    ON portfolio_snapshots (portfolio_id, as_of_event_time DESC, as_of_event_id DESC);
