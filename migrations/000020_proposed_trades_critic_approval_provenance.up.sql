-- Critic audit (Phase 3) and approval provenance: human vs future paper auto-approve.
-- NULL approval_source = legacy rows or not yet approved.

ALTER TABLE proposed_trades
    ADD COLUMN critic_verdict JSONB NULL,
    ADD COLUMN critic_completed_at TIMESTAMPTZ NULL,
    ADD COLUMN critic_model TEXT NULL,
    ADD COLUMN approval_source TEXT NULL;

ALTER TABLE proposed_trades
    ADD CONSTRAINT chk_proposed_trades_approval_source
    CHECK (approval_source IS NULL OR approval_source IN ('human', 'paper_auto'));
