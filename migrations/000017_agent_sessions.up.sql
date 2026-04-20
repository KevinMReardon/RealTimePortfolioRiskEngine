CREATE TABLE IF NOT EXISTS agent_sessions (
    session_id UUID PRIMARY KEY,
    portfolio_id UUID NOT NULL,
    requested_by_user_id UUID NULL,
    trigger_source TEXT NOT NULL,
    run_date DATE NOT NULL,
    status TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    temperature NUMERIC(6,4) NULL,
    max_tokens INT NULL,
    system_prompt TEXT NOT NULL,
    user_prompt JSONB NOT NULL,
    response_raw JSONB NULL,
    response_validated JSONB NULL,
    validation_errors JSONB NULL,
    input_tokens INT NULL,
    output_tokens INT NULL,
    tool_call_count INT NOT NULL DEFAULT 0,
    estimated_cost_usd NUMERIC(12,6) NULL,
    error_code TEXT NULL,
    error_message TEXT NULL,
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_agent_sessions_portfolio
        FOREIGN KEY (portfolio_id) REFERENCES portfolios(portfolio_id) ON DELETE CASCADE,
    CONSTRAINT fk_agent_sessions_requested_by_user
        FOREIGN KEY (requested_by_user_id) REFERENCES users(user_id) ON DELETE SET NULL,
    CONSTRAINT chk_agent_sessions_trigger_source
        CHECK (trigger_source IN ('manual', 'scheduled')),
    CONSTRAINT chk_agent_sessions_status
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'invalid_output', 'rate_limited'))
);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_portfolio_created
    ON agent_sessions (portfolio_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_status_created
    ON agent_sessions (status, created_at);

CREATE INDEX IF NOT EXISTS idx_agent_sessions_trigger_run_date
    ON agent_sessions (trigger_source, run_date, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_sessions_daily_portfolio_scheduled
    ON agent_sessions (portfolio_id, run_date)
    WHERE trigger_source = 'scheduled';
