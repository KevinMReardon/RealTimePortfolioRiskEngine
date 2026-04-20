-- Per-portfolio bookmark for Alpaca REST sync (activities pagination + incremental watermark).
CREATE TABLE IF NOT EXISTS alpaca_sync_state (
    portfolio_id UUID PRIMARY KEY REFERENCES portfolios (portfolio_id) ON DELETE CASCADE,
    last_success_at TIMESTAMPTZ NULL,
    activities_page_token TEXT NULL,
    activities_after_time TIMESTAMPTZ NULL,
    last_error TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_alpaca_sync_state_updated_at
    ON alpaca_sync_state (updated_at DESC);
