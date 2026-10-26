-- PR-1068 (Phase 12): purchasability gate for products that must be sold
-- through an assignment context. DEFAULT false backfills every existing row
-- (constant DEFAULT on ADD COLUMN). The flag is independent of product type
-- and of linked-eligibility rule rows. A boolean needs no CHECK and no
-- separate VALIDATE migration.

ALTER TABLE products ADD COLUMN requires_linked_parent BOOLEAN NOT NULL DEFAULT false;
