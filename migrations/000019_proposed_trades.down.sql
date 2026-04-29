DROP INDEX IF EXISTS idx_portfolio_equity_anchor_anchor_date;

DROP TABLE IF EXISTS portfolio_equity_anchor;

DROP INDEX IF EXISTS idx_kill_switch_events_created_at;

DROP TABLE IF EXISTS kill_switch_events;

DROP INDEX IF EXISTS uq_proposed_trades_session_idea_index;

DROP INDEX IF EXISTS idx_proposed_trades_symbol;

DROP INDEX IF EXISTS idx_proposed_trades_portfolio_status_created;

DROP TABLE IF EXISTS proposed_trades;
