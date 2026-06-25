DROP INDEX IF EXISTS idx_proposed_trades_session_status_retry;

ALTER TABLE proposed_trades
    DROP CONSTRAINT IF EXISTS chk_proposed_trades_status;

ALTER TABLE proposed_trades
    ADD CONSTRAINT chk_proposed_trades_status
        CHECK (status IN ('proposed', 'approved', 'submitted', 'filled', 'rejected', 'cancelled'));

ALTER TABLE proposed_trades
    DROP COLUMN IF EXISTS paper_auto_retry_count;
