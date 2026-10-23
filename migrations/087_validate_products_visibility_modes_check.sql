-- PR-1055: VALIDATE products_visibility_modes_check after ADD (086) committed.
-- SHARE UPDATE EXCLUSIVE scan; do not combine with 086 (ACCESS EXCLUSIVE ADD).
-- Apply before deploying app code that SELECTs the new visibility mode columns.
ALTER TABLE products VALIDATE CONSTRAINT products_visibility_modes_check;
