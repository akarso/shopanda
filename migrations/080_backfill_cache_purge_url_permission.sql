-- Backfill role_permissions for cache.purge_url (PR-1046).
-- Same class of gap as 075 / 079: compiled defaults in
-- internal/domain/rbac/role_permissions.go are not the source of truth
-- once rbac.InitEffectivePermissions has run (every server startup via
-- adminrole.Service.SyncPluginDefaults). SyncPluginDefaults only
-- backfills plugin-registered permissions, not core ones, so a new core
-- permission missing from this table is silently unenforceable for the
-- admin role until an operator adds the row by hand.
--
-- Admin-only, matching the compiled defaults. ON CONFLICT DO NOTHING:
-- safe if an operator already granted this via the roles UI/API.
INSERT INTO role_permissions (role, permission) VALUES
    ('admin', 'cache.purge_url')
ON CONFLICT DO NOTHING;
