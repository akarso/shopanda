-- PR-1069 (Phase 12): instance-level ProductAssignment.
-- Binds one purchased/issued child unit to one purchased parent unit.
-- Units are opaque TEXT ids (an order line, or a serialized digital unit).
-- order_items has a composite PK (order_id, variant_id) and no unique id,
-- and a digital "card in a phone" is not an order line, so these columns
-- are not foreign keys. PR-1070 decides how ids are minted.
-- Spec named the table singular (product_assignment); plural matches
-- orders / order_items / linked_eligible_parents.
-- Status machine: unassigned → assigned → revoked, or unassigned →
-- revoked when a never-assigned unit is released (cancel/return before
-- anyone assigns it). Revoke is not a delete. A revoked row stays;
-- reassignment inserts a new assigned row. At most one current
-- (unassigned or assigned) row per child. A revoked never-assigned row
-- keeps parent NULL so CurrentByChild is not "still live."

CREATE TABLE IF NOT EXISTS product_assignments (
    id                   UUID PRIMARY KEY,
    parent_order_item_id TEXT,
    child_order_item_id  TEXT NOT NULL,
    status               TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at           TIMESTAMPTZ,
    CONSTRAINT product_assignments_status_check
        CHECK (status IN ('unassigned', 'assigned', 'revoked')),
    CONSTRAINT product_assignments_state_check
        CHECK (
            (status = 'unassigned' AND parent_order_item_id IS NULL AND revoked_at IS NULL)
            OR (status = 'assigned' AND parent_order_item_id IS NOT NULL
                AND btrim(parent_order_item_id) <> '' AND revoked_at IS NULL)
            OR (status = 'revoked' AND revoked_at IS NOT NULL
                AND (parent_order_item_id IS NULL
                    OR btrim(parent_order_item_id) <> ''))
        ),
    CONSTRAINT product_assignments_not_self_check
        CHECK (parent_order_item_id IS NULL OR parent_order_item_id <> child_order_item_id),
    CONSTRAINT product_assignments_child_not_empty_check
        CHECK (btrim(child_order_item_id) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS product_assignments_child_current
    ON product_assignments (child_order_item_id)
    WHERE status IN ('unassigned', 'assigned');

CREATE INDEX IF NOT EXISTS product_assignments_child_created
    ON product_assignments (child_order_item_id, created_at DESC);

CREATE INDEX IF NOT EXISTS product_assignments_parent
    ON product_assignments (parent_order_item_id)
    WHERE parent_order_item_id IS NOT NULL;
