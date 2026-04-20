-- Dashboard query indexes for admin stats overview.
CREATE INDEX IF NOT EXISTS idx_orders_created_at_desc ON orders (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_stock_low_quantity ON stock (quantity) WHERE quantity > 0;
