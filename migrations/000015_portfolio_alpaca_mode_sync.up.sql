ALTER TABLE portfolios
    ADD COLUMN IF NOT EXISTS alpaca_account_mode TEXT NOT NULL DEFAULT 'paper',
    ADD COLUMN IF NOT EXISTS alpaca_sync_enabled BOOLEAN NOT NULL DEFAULT true;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'chk_portfolios_alpaca_account_mode'
    ) THEN
        ALTER TABLE portfolios
            ADD CONSTRAINT chk_portfolios_alpaca_account_mode
            CHECK (alpaca_account_mode IN ('paper', 'live'));
    END IF;
END $$;
