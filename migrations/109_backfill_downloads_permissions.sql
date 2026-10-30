-- Backfill role_permissions for downloads.read / downloads.write (PR-1078).
-- Same class of gap as 101 (assignments): compiled defaults in
-- internal/domain/rbac/role_permissions.go are not the source of truth
-- once rbac.InitEffectivePermissions has run. SyncPluginDefaults only
-- backfills plugin-registered permissions, not core ones.
--
-- Admin, manager, and support. Editor is intentionally omitted: catalog
-- write does not include revoking or re-issuing a customer's grant.
-- Support can restore download access without products.write.
-- File attach / grant-settings stay on products.write.
INSERT INTO role_permissions (role, permission) VALUES
    ('admin', 'downloads.read'),
    ('admin', 'downloads.write'),
    ('manager', 'downloads.read'),
    ('manager', 'downloads.write'),
    ('support', 'downloads.read'),
    ('support', 'downloads.write')
ON CONFLICT DO NOTHING;
