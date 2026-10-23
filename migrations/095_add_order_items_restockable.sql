-- PR-1064: snapshot whether a returned order line may increment stock.
-- DEFAULT true backfills every existing row (constant DEFAULT on ADD COLUMN).
-- The first UPDATE clears historical virtual and downloadable lines. The
-- second clears lines whose variant row is gone: a missing catalog match is
-- not restockable. Checkout writes the flag explicitly for new orders.
-- Receive trusts this column and does not re-read the live product type.
-- A boolean needs no CHECK and no separate VALIDATE migration.

ALTER TABLE order_items ADD COLUMN restockable BOOLEAN NOT NULL DEFAULT true;

UPDATE order_items AS oi
SET restockable = false
FROM variants AS v
JOIN products AS p ON p.id = v.product_id
WHERE oi.variant_id = v.id::text
  AND p.type IN ('virtual', 'downloadable');

-- order_items.variant_id is text and has no FK to variants. A deleted variant
-- leaves the line behind. LEFT JOIN ... WHERE v.id IS NULL marks those rows.
UPDATE order_items AS oi
SET restockable = false
WHERE (oi.order_id, oi.variant_id) IN (
    SELECT src.order_id, src.variant_id
    FROM order_items AS src
    LEFT JOIN variants AS v ON v.id::text = src.variant_id
    WHERE v.id IS NULL
);
