-- Alpaca brokerage account id (from GET /v2/account) linked to this catalog portfolio for single-book deployments.
ALTER TABLE portfolios
    ADD COLUMN IF NOT EXISTS alpaca_account_id TEXT NULL;
