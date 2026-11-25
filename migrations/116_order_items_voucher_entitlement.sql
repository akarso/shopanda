-- PR-1802 follow-up: snapshot voucher entitlement at checkout so order.paid
-- issues from what the shopper bought, not live voucher_settings at webhook time.
ALTER TABLE order_items
    ADD COLUMN IF NOT EXISTS voucher_codes_per_unit INT,
    ADD COLUMN IF NOT EXISTS voucher_max_redemptions INT;

ALTER TABLE order_items
    DROP CONSTRAINT IF EXISTS order_items_voucher_codes_per_unit_range;
ALTER TABLE order_items
    ADD CONSTRAINT order_items_voucher_codes_per_unit_range
    CHECK (voucher_codes_per_unit IS NULL OR (voucher_codes_per_unit >= 1 AND voucher_codes_per_unit <= 50));

ALTER TABLE order_items
    DROP CONSTRAINT IF EXISTS order_items_voucher_max_redemptions_range;
ALTER TABLE order_items
    ADD CONSTRAINT order_items_voucher_max_redemptions_range
    CHECK (voucher_max_redemptions IS NULL OR (voucher_max_redemptions >= 1 AND voucher_max_redemptions <= 1000));
