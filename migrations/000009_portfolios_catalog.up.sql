-- portfolio catalog for user-facing create/list operations.
CREATE TABLE IF NOT EXISTS portfolios (
    portfolio_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    base_currency TEXT NOT NULL DEFAULT 'USD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_portfolios_created_at
    ON portfolios (created_at DESC);

-- Backfill catalog from existing event stream so historical portfolios are visible.
INSERT INTO portfolios (portfolio_id, name, base_currency, created_at, updated_at)
SELECT DISTINCT e.portfolio_id,
       'Portfolio ' || LEFT(e.portfolio_id::text, 8),
       'USD',
       NOW(),
       NOW()
FROM events e
ON CONFLICT (portfolio_id) DO NOTHING;
