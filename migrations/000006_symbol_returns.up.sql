-- Derived daily returns per symbol for rolling volatility inputs.
CREATE TABLE IF NOT EXISTS symbol_returns (
    symbol TEXT NOT NULL,
    return_date DATE NOT NULL,
    close_price NUMERIC(20,8) NOT NULL,
    daily_return NUMERIC(20,10),
    as_of_event_time TIMESTAMPTZ NOT NULL,
    as_of_event_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (symbol, return_date)
);

CREATE INDEX IF NOT EXISTS idx_symbol_returns_symbol_date_desc
    ON symbol_returns (symbol, return_date DESC);
