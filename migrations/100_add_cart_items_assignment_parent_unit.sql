-- PR-1070: persist the selected parent instance on the cart line so
-- checkout can CreateAssigned against that unit. Empty string is "no
-- instance selected" (deferred or product-only hint).

ALTER TABLE cart_items ADD COLUMN assignment_parent_unit_id TEXT NOT NULL DEFAULT '';
