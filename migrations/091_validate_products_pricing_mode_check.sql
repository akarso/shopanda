-- PR-1062: VALIDATE products_pricing_mode_check after ADD (090) committed.
-- SHARE UPDATE EXCLUSIVE scan; do not combine with 090 (ACCESS EXCLUSIVE ADD).
-- Apply before deploying app code that SELECTs pricing_mode.
ALTER TABLE products VALIDATE CONSTRAINT products_pricing_mode_check;
