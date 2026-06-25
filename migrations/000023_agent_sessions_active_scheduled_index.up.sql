CREATE INDEX IF NOT EXISTS idx_agent_sessions_active_scheduled_portfolio
ON agent_sessions (portfolio_id, (COALESCE(started_at, created_at)) DESC, created_at DESC)
WHERE trigger_source = 'scheduled'
  AND status IN ('queued', 'running');
