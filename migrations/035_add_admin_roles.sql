ALTER TABLE customers
    DROP CONSTRAINT customers_role_check,
    ADD CONSTRAINT customers_role_check CHECK (role IN ('customer', 'admin', 'manager', 'editor', 'support'));
