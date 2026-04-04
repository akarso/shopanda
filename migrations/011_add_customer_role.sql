ALTER TABLE customers
    ADD COLUMN role TEXT NOT NULL DEFAULT 'customer'
    CONSTRAINT customers_role_check CHECK (role IN ('customer', 'admin'));
