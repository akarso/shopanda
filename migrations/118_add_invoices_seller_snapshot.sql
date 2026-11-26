-- Seal invoice seller identity at issue time (PR-1205 CR).
-- Live store identity must not rewrite historical tax documents on re-render.
ALTER TABLE invoices
    ADD COLUMN seller_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;
