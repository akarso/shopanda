-- PR-1071: record an operator force-bind so a later reader can see it.
-- Checkout and self-service binds leave both columns null.
-- AdminAssign sets them together only when a side is not bindable. Revoke keeps them.
-- Existing rows satisfy the check (both null).

ALTER TABLE product_assignments
    ADD COLUMN IF NOT EXISTS forced_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS forced_reason TEXT NULL;

ALTER TABLE product_assignments
    DROP CONSTRAINT IF EXISTS product_assignments_forced_check;

ALTER TABLE product_assignments
    ADD CONSTRAINT product_assignments_forced_check
    CHECK (
        (forced_at IS NULL AND forced_reason IS NULL)
        OR (
            forced_at IS NOT NULL
            AND forced_reason IS NOT NULL
            AND btrim(forced_reason) <> ''
            AND status IN ('assigned', 'revoked')
        )
    );
