ALTER TABLE portfolios
    DROP CONSTRAINT IF EXISTS chk_portfolios_alpaca_account_mode;

ALTER TABLE portfolios
    DROP COLUMN IF EXISTS alpaca_sync_enabled,
    DROP COLUMN IF EXISTS alpaca_account_mode;
