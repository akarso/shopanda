ALTER TABLE customers
    ADD COLUMN role TEXT NOT NULL DEFAULT 'customer'
    CHECK (role IN ('customer', 'admin'));
