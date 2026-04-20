CREATE TABLE IF NOT EXISTS alpaca_import_jobs (
    job_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    portfolio_id UUID NOT NULL REFERENCES portfolios(portfolio_id) ON DELETE CASCADE,
    requested_by_user_id UUID REFERENCES users(user_id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    since_ts TIMESTAMPTZ,
    until_ts TIMESTAMPTZ,
    full_history BOOLEAN NOT NULL DEFAULT FALSE,
    inserted_count INTEGER NOT NULL DEFAULT 0,
    duplicate_count INTEGER NOT NULL DEFAULT 0,
    skipped_invalid_count INTEGER NOT NULL DEFAULT 0,
    pages_fetched INTEGER NOT NULL DEFAULT 0,
    activities_seen INTEGER NOT NULL DEFAULT 0,
    fills_considered INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_alpaca_import_jobs_portfolio_created
    ON alpaca_import_jobs(portfolio_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_alpaca_import_jobs_status_created
    ON alpaca_import_jobs(status, created_at);

-- At most one queued or running job per portfolio (race-safe vs SELECT-then-insert).
CREATE UNIQUE INDEX IF NOT EXISTS uq_alpaca_import_jobs_one_active_per_portfolio
    ON alpaca_import_jobs (portfolio_id)
    WHERE status IN ('queued', 'running');
