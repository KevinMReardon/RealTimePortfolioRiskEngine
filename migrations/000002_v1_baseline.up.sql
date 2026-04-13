-- v1 baseline: core storage schema for events, dlq, projections, and snapshots.

-- Canonical events table (append-only)
CREATE TABLE IF NOT EXISTS events (
    event_id UUID PRIMARY KEY,
    portfolio_id UUID NOT NULL,
    event_time TIMESTAMPTZ NOT NULL,
    event_type TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    source TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Deterministic replay index
CREATE INDEX IF NOT EXISTS idx_events_portfolio_event_time_id
    ON events (portfolio_id, event_time, event_id);

-- Idempotency unique index (scope can be adjusted per LLD, e.g. (source, idempotency_key))
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_portfolio_idempotency_key
    ON events (portfolio_id, idempotency_key);


-- Dead-letter queue for failed events
CREATE TABLE IF NOT EXISTS dlq_events (
    id BIGSERIAL PRIMARY KEY,
    original_event_id UUID,
    portfolio_id UUID,
    failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    error_message TEXT,
    payload JSONB,
    metadata JSONB
);


-- Current positions per (portfolio_id, symbol)
CREATE TABLE IF NOT EXISTS positions_projection (
    portfolio_id UUID NOT NULL,
    symbol TEXT NOT NULL,
    quantity NUMERIC(20, 8) NOT NULL,
    cost_basis NUMERIC(20, 8) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (portfolio_id, symbol)
);


-- Last price per symbol
CREATE TABLE IF NOT EXISTS prices_projection (
    symbol TEXT PRIMARY KEY,
    price NUMERIC(20, 8) NOT NULL,
    as_of TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);


-- Optional periodic portfolio checkpoints
CREATE TABLE IF NOT EXISTS portfolio_snapshots (
    id BIGSERIAL PRIMARY KEY,
    portfolio_id UUID NOT NULL,
    as_of_event_time TIMESTAMPTZ NOT NULL,
    as_of_event_id UUID NOT NULL,
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_portfolio_snapshots_portfolio_as_of
    ON portfolio_snapshots (portfolio_id, as_of_event_time DESC);


-- Optional materialized risk snapshots
CREATE TABLE IF NOT EXISTS risk_snapshots (
    id BIGSERIAL PRIMARY KEY,
    portfolio_id UUID NOT NULL,
    as_of_event_time TIMESTAMPTZ NOT NULL,
    as_of_event_id UUID NOT NULL,
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_risk_snapshots_portfolio_as_of
    ON risk_snapshots (portfolio_id, as_of_event_time DESC);
