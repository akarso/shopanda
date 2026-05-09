ALTER TABLE orders
    ADD COLUMN contact_email TEXT;

CREATE INDEX idx_orders_contact_email ON orders (contact_email);
