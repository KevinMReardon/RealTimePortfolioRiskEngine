-- Persist price-mark driving event id for honest cross-projection reads (LLD §14.1).
ALTER TABLE prices_projection
    ADD COLUMN IF NOT EXISTS as_of_event_id UUID;
