-- PR-1061 (Phase 12): grouped product display membership.
-- group_product_id must reference a Type = grouped product (enforced in application).
-- member_product_id must be a leaf type (simple, virtual, downloadable) in application.
-- Both FKs stay ON DELETE CASCADE, including member_product_id. This row is a
-- display link: deleting a SKU silently unlinks it from every group, and
-- deleting the group product removes its membership rows. Member products
-- themselves are not deleted. A future product delete API must not return
-- 422 because of grouped membership — silent unlink is intentional for members
-- (and for the group parent). Contrast bundle_components, where the component
-- side is ON DELETE RESTRICT.
-- It does not gate the member's own Purchasable or visibility.
-- The same member_product_id may appear in many groups (PK is the pair).

CREATE TABLE IF NOT EXISTS grouped_members (
    group_product_id  UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    member_product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_product_id, member_product_id),
    CHECK (group_product_id <> member_product_id)
);

CREATE INDEX IF NOT EXISTS idx_grouped_members_member
    ON grouped_members (member_product_id);
