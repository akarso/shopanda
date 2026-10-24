-- PR-1067 (Phase 12): linked-product catalog eligibility.
-- Child products declare which parents they may later be assigned to.
-- Two scoping mechanisms, both optional, OR-combined at evaluation:
--   1. explicit parent-product allowlist (linked_eligible_parents)
--   2. category membership (linked_eligible_categories + product_categories)
-- Eligibility is not composition (bundle/grouped) and does not gate
-- Purchasable (PR-1068) or record instance assignment (PR-1069).
-- Type rules stay in application; these tables do not store product types.
-- Spec named the allowlist table singular (linked_eligible_parent); plural
-- matches bundle_components / grouped_members.

CREATE TABLE IF NOT EXISTS linked_eligible_parents (
    child_product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    parent_product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_product_id, parent_product_id),
    CHECK (child_product_id <> parent_product_id)
);

CREATE INDEX IF NOT EXISTS idx_linked_eligible_parents_parent
    ON linked_eligible_parents (parent_product_id);

CREATE TABLE IF NOT EXISTS linked_eligible_categories (
    child_product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    category_id      TEXT NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (child_product_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_linked_eligible_categories_category
    ON linked_eligible_categories (category_id);
