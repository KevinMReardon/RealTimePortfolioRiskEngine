ALTER TABLE portfolios
    ADD COLUMN IF NOT EXISTS alpaca_key_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS alpaca_secret_key TEXT NULL,
    ADD COLUMN IF NOT EXISTS alpaca_base_url TEXT NULL;
