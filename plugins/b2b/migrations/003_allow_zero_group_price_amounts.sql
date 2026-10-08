-- PR-1048 (Phase 12): NewGroupPrice accepts amount 0. Align the B2B
-- customer_group_prices CHECK so admin/group upserts can store free prices.

ALTER TABLE customer_group_prices ADD CONSTRAINT customer_group_prices_amount_check_new
    CHECK (amount >= 0) NOT VALID;
ALTER TABLE customer_group_prices DROP CONSTRAINT customer_group_prices_amount_check;
ALTER TABLE customer_group_prices RENAME CONSTRAINT customer_group_prices_amount_check_new TO customer_group_prices_amount_check;
ALTER TABLE customer_group_prices VALIDATE CONSTRAINT customer_group_prices_amount_check;
