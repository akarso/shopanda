-- Persist checkout destination country and tax total for OSS/IOSS reporting.
ALTER TABLE orders
    ADD COLUMN destination_country TEXT
        CHECK (destination_country IS NULL OR length(destination_country) = 2),
    ADD COLUMN tax_amount BIGINT
        CHECK (tax_amount IS NULL OR tax_amount >= 0);

CREATE INDEX idx_orders_paid_tax_snapshot_created
    ON orders (created_at, id)
    WHERE status = 'paid' AND destination_country IS NOT NULL;
