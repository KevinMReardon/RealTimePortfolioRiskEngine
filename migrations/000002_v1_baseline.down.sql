-- v1 baseline rollback: drop indexes and tables in a safe order.

-- Drop indexes if they exist
DROP INDEX IF EXISTS idx_risk_snapshots_portfolio_as_of;
DROP INDEX IF EXISTS idx_portfolio_snapshots_portfolio_as_of;
DROP INDEX IF EXISTS idx_events_portfolio_event_time_id;
DROP INDEX IF EXISTS idx_events_portfolio_idempotency_key;

-- Drop tables in reverse dependency / usage order
DROP TABLE IF EXISTS risk_snapshots;
DROP TABLE IF EXISTS portfolio_snapshots;
DROP TABLE IF EXISTS prices_projection;
DROP TABLE IF EXISTS positions_projection;
DROP TABLE IF EXISTS dlq_events;
DROP TABLE IF EXISTS events;
