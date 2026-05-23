ALTER TABLE proposed_trades DROP CONSTRAINT IF EXISTS chk_proposed_trades_approval_source;

ALTER TABLE proposed_trades
    DROP COLUMN IF EXISTS approval_source,
    DROP COLUMN IF EXISTS critic_model,
    DROP COLUMN IF EXISTS critic_completed_at,
    DROP COLUMN IF EXISTS critic_verdict;
