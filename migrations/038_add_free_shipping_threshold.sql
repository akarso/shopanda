ALTER TABLE shipping_zones
    ADD COLUMN free_shipping_threshold BIGINT NOT NULL DEFAULT 0 CHECK (free_shipping_threshold >= 0),
    ADD COLUMN free_shipping_currency  TEXT   NOT NULL DEFAULT '';
