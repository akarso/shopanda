-- Backfill role_permissions for assignments.read / assignments.write (PR-1071).
-- Same class of gap as 075 / 079 / 080: compiled defaults in
-- internal/domain/rbac/role_permissions.go are not the source of truth
-- once rbac.InitEffectivePermissions has run (every server startup via
-- adminrole.Service.SyncPluginDefaults). SyncPluginDefaults only
-- backfills plugin-registered permissions, not core ones, so a new core
-- permission missing from this table is silently unenforceable until an
-- operator adds the row by hand.
--
-- Admin, manager, and support. Editor is intentionally omitted: catalog
-- write does not include correcting a customer's unit binding. Support
-- can correct a binding without products.write. ON CONFLICT DO NOTHING
-- is safe if an operator already granted these via the roles UI/API.
INSERT INTO role_permissions (role, permission) VALUES
    ('admin', 'assignments.read'),
    ('admin', 'assignments.write'),
    ('manager', 'assignments.read'),
    ('manager', 'assignments.write'),
    ('support', 'assignments.read'),
    ('support', 'assignments.write')
ON CONFLICT DO NOTHING;
