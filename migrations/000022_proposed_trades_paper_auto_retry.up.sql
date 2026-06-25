-- Paper-auto retry attempt tracking and terminal auto_abandoned status.

ALTER TABLE proposed_trades
    ADD COLUMN IF NOT EXISTS paper_auto_retry_count INT NOT NULL DEFAULT 0;

ALTER TABLE proposed_trades
    DROP CONSTRAINT IF EXISTS chk_proposed_trades_status;

ALTER TABLE proposed_trades
    ADD CONSTRAINT chk_proposed_trades_status
        CHECK (status IN (
            'proposed',
            'approved',
            'submitted',
            'filled',
            'rejected',
            'cancelled',
            'auto_abandoned'
        ));

CREATE INDEX IF NOT EXISTS idx_proposed_trades_session_status_retry
    ON proposed_trades (portfolio_id, agent_session_id, status)
    WHERE agent_session_id IS NOT NULL
      AND status IN ('proposed', 'approved');
