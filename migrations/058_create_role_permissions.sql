-- Editable admin role permission assignments (core defaults seeded below).

CREATE TABLE role_permissions (
    role        TEXT NOT NULL CHECK (role IN ('admin', 'manager', 'editor', 'support')),
    permission  TEXT NOT NULL,
    PRIMARY KEY (role, permission)
);

CREATE INDEX idx_role_permissions_role ON role_permissions (role);

-- Seed core defaults matching internal/domain/rbac/role_permissions.go.
INSERT INTO role_permissions (role, permission) VALUES
    ('admin', 'products.read'),
    ('admin', 'products.write'),
    ('admin', 'orders.read'),
    ('admin', 'orders.write'),
    ('admin', 'categories.read'),
    ('admin', 'categories.write'),
    ('admin', 'customers.read'),
    ('admin', 'customers.write'),
    ('admin', 'invoices.read'),
    ('admin', 'media.read'),
    ('admin', 'media.write'),
    ('admin', 'content.read'),
    ('admin', 'content.write'),
    ('admin', 'settings.read'),
    ('admin', 'settings.write'),
    ('admin', 'shipping.read'),
    ('admin', 'shipping.write'),
    ('admin', 'audit.read'),
    ('manager', 'products.read'),
    ('manager', 'products.write'),
    ('manager', 'orders.read'),
    ('manager', 'orders.write'),
    ('manager', 'categories.read'),
    ('manager', 'categories.write'),
    ('manager', 'customers.read'),
    ('manager', 'invoices.read'),
    ('manager', 'media.read'),
    ('manager', 'media.write'),
    ('manager', 'content.read'),
    ('manager', 'shipping.read'),
    ('manager', 'shipping.write'),
    ('editor', 'products.read'),
    ('editor', 'products.write'),
    ('editor', 'categories.read'),
    ('editor', 'categories.write'),
    ('editor', 'media.read'),
    ('editor', 'media.write'),
    ('editor', 'content.read'),
    ('editor', 'content.write'),
    ('support', 'products.read'),
    ('support', 'orders.read'),
    ('support', 'customers.read'),
    ('support', 'invoices.read'),
    ('support', 'content.read');
