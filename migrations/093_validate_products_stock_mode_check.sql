-- PR-1063: VALIDATE products_stock_mode_check after ADD (092) committed.
-- SHARE UPDATE EXCLUSIVE scan; do not combine with 092 (ACCESS EXCLUSIVE ADD).
-- Apply before deploying app code that SELECTs stock_mode.
ALTER TABLE products VALIDATE CONSTRAINT products_stock_mode_check;
