-- PR-1049 (Phase 12): validate products_type_check installed NOT VALID in 083.
-- Separate transaction so ACCESS EXCLUSIVE from 083 is released first.
-- Existing rows are already 'simple' from the ADD COLUMN DEFAULT; VALIDATE
-- mainly flips the constraint to convalidated for future writes.

ALTER TABLE products VALIDATE CONSTRAINT products_type_check;
