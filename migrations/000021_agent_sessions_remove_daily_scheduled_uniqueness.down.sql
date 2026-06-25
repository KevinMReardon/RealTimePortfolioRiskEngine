CREATE UNIQUE INDEX IF NOT EXISTS uq_agent_sessions_daily_portfolio_scheduled
    ON agent_sessions (portfolio_id, run_date);
