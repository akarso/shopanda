-- PR-1048 (Phase 12): validate CHECK installed NOT VALID in 003.
-- Separate transaction so ACCESS EXCLUSIVE from 003 is released first.

ALTER TABLE customer_group_prices VALIDATE CONSTRAINT customer_group_prices_amount_check;
