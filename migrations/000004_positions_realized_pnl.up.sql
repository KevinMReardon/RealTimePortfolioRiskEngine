-- §7: persist cumulative realized PnL per (portfolio_id, symbol). cost_basis column stores weighted average cost.
ALTER TABLE positions_projection
    ADD COLUMN realized_pnl NUMERIC(20, 8) NOT NULL DEFAULT 0;
