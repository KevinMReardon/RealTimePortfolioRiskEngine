-- Phase 2: proposed trade lifecycle, global kill-switch audit, daily equity anchors (NY calendar).

CREATE TABLE IF NOT EXISTS proposed_trades (
    proposal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    portfolio_id UUID NOT NULL,
    agent_session_id UUID NULL,
    trade_idea_index INT NULL,
    idea_fingerprint TEXT NULL,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    quantity NUMERIC NULL,
    notional_usd NUMERIC NULL,
    order_type TEXT NULL,
    limit_price NUMERIC NULL,
    time_in_force TEXT NULL,
    client_order_id TEXT NULL,
    rationale_snapshot TEXT NULL,
    policy_result JSONB NOT NULL,
    policy_inputs_hash TEXT NOT NULL,
    policy_config_hash TEXT NOT NULL,
    payload_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    row_version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    approved_by_user_id UUID NULL,
    approved_at TIMESTAMPTZ NULL,
    denied_by_user_id UUID NULL,
    deny_reason TEXT NULL,
    submitted_at TIMESTAMPTZ NULL,
    broker_order_id TEXT NULL,
    last_error TEXT NULL,
    CONSTRAINT fk_proposed_trades_portfolio
        FOREIGN KEY (portfolio_id) REFERENCES portfolios(portfolio_id) ON DELETE CASCADE,
    CONSTRAINT fk_proposed_trades_agent_session
        FOREIGN KEY (agent_session_id) REFERENCES agent_sessions(session_id) ON DELETE SET NULL,
    CONSTRAINT fk_proposed_trades_approved_by_user
        FOREIGN KEY (approved_by_user_id) REFERENCES users(user_id) ON DELETE SET NULL,
    CONSTRAINT fk_proposed_trades_denied_by_user
        FOREIGN KEY (denied_by_user_id) REFERENCES users(user_id) ON DELETE SET NULL,
    CONSTRAINT chk_proposed_trades_status
        CHECK (status IN ('proposed', 'approved', 'submitted', 'filled', 'rejected', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_proposed_trades_portfolio_status_created
    ON proposed_trades (portfolio_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_proposed_trades_symbol
    ON proposed_trades (symbol);

CREATE UNIQUE INDEX IF NOT EXISTS uq_proposed_trades_session_idea_index
    ON proposed_trades (agent_session_id, trade_idea_index)
    WHERE agent_session_id IS NOT NULL AND trade_idea_index IS NOT NULL;

CREATE TABLE IF NOT EXISTS kill_switch_events (
    event_id BIGSERIAL PRIMARY KEY,
    active BOOLEAN NOT NULL,
    reason TEXT NOT NULL,
    toggled_by_user_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_kill_switch_events_toggled_by_user
        FOREIGN KEY (toggled_by_user_id) REFERENCES users(user_id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_kill_switch_events_created_at
    ON kill_switch_events (created_at DESC);

CREATE TABLE IF NOT EXISTS portfolio_equity_anchor (
    portfolio_id UUID NOT NULL,
    anchor_date DATE NOT NULL,
    equity NUMERIC NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT pk_portfolio_equity_anchor PRIMARY KEY (portfolio_id, anchor_date),
    CONSTRAINT fk_portfolio_equity_anchor_portfolio
        FOREIGN KEY (portfolio_id) REFERENCES portfolios(portfolio_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_portfolio_equity_anchor_anchor_date
    ON portfolio_equity_anchor (anchor_date DESC);
