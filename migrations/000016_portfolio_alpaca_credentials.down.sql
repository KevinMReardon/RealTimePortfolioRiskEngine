ALTER TABLE portfolios
    DROP COLUMN IF EXISTS alpaca_base_url,
    DROP COLUMN IF EXISTS alpaca_secret_key,
    DROP COLUMN IF EXISTS alpaca_key_id;
