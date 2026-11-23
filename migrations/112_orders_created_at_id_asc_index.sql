-- ASC keyset for OrderRepository.ListCreatedSince / OrderPort reconciliation.
CREATE INDEX IF NOT EXISTS idx_orders_created_at_id_asc ON orders (created_at ASC, id ASC);
