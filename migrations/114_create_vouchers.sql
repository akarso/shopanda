-- PR-1800 (Phase 18): Voucher settings, issued codes, and redemption audit.
-- Virtual products with voucher_settings.enabled issue codes on order.paid.
-- Codes are staff-redeemed (box office / till), not cart discounts or store credit.

CREATE TABLE IF NOT EXISTS voucher_settings (
    product_id       UUID PRIMARY KEY REFERENCES products(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    codes_per_unit   INT NOT NULL DEFAULT 1,
    max_redemptions  INT NOT NULL DEFAULT 1,
    CONSTRAINT voucher_settings_codes_per_unit_range
        CHECK (codes_per_unit >= 1 AND codes_per_unit <= 50),
    CONSTRAINT voucher_settings_max_redemptions_range
        CHECK (max_redemptions >= 1 AND max_redemptions <= 1000)
);

CREATE TABLE IF NOT EXISTS vouchers (
    id               UUID PRIMARY KEY,
    code             TEXT NOT NULL,
    order_id         UUID NOT NULL REFERENCES orders(id) ON DELETE RESTRICT,
    order_item_id    TEXT NOT NULL,
    product_id       UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    variant_id       TEXT NOT NULL,
    customer_id      TEXT NOT NULL DEFAULT '',
    seq              INT NOT NULL DEFAULT 0,
    max_redemptions  INT NOT NULL,
    redeemed_count   INT NOT NULL DEFAULT 0,
    voided_at        TIMESTAMPTZ,
    issued_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT vouchers_code_not_empty
        CHECK (btrim(code) <> ''),
    CONSTRAINT vouchers_order_item_not_empty
        CHECK (btrim(order_item_id) <> ''),
    CONSTRAINT vouchers_seq_nonneg
        CHECK (seq >= 0),
    CONSTRAINT vouchers_max_redemptions_range
        CHECK (max_redemptions >= 1 AND max_redemptions <= 1000),
    CONSTRAINT vouchers_redeemed_count_range
        CHECK (redeemed_count >= 0 AND redeemed_count <= max_redemptions),
    CONSTRAINT vouchers_code_unique
        UNIQUE (code),
    CONSTRAINT vouchers_order_variant_seq_unique
        UNIQUE (order_id, variant_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_vouchers_order
    ON vouchers (order_id);

CREATE INDEX IF NOT EXISTS idx_vouchers_customer
    ON vouchers (customer_id)
    WHERE customer_id <> '';

CREATE INDEX IF NOT EXISTS idx_vouchers_product
    ON vouchers (product_id);

CREATE TABLE IF NOT EXISTS voucher_redemptions (
    id          UUID PRIMARY KEY,
    voucher_id  UUID NOT NULL REFERENCES vouchers(id) ON DELETE CASCADE,
    action      TEXT NOT NULL,
    actor_id    TEXT NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT voucher_redemptions_action_check
        CHECK (action IN ('redeem', 'unredeem')),
    CONSTRAINT voucher_redemptions_actor_not_empty
        CHECK (btrim(actor_id) <> '')
);

CREATE INDEX IF NOT EXISTS idx_voucher_redemptions_voucher
    ON voucher_redemptions (voucher_id, created_at DESC);
