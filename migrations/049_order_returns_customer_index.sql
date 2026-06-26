-- Index for customer account return listings.

CREATE INDEX idx_order_returns_customer ON order_returns (customer_id, created_at DESC);
