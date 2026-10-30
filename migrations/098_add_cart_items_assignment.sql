-- PR-1068: persist assignment context on cart lines so claim, qty
-- increase, and checkout can re-read the same parent/deferred stamp.
-- Empty parent + deferred false is "no stored context" (request fallback).

ALTER TABLE cart_items ADD COLUMN assignment_parent_product_id TEXT NOT NULL DEFAULT '';
ALTER TABLE cart_items ADD COLUMN assignment_deferred BOOLEAN NOT NULL DEFAULT false;
