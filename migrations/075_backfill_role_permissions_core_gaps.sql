-- Backfills role_permissions rows for core permissions added to
-- internal/domain/rbac/role_permissions.go after
-- 058_create_role_permissions.sql shipped, and never added to this table
-- by any later migration: customers.store_credit.write, extensions.read,
-- extensions.write, extensions.private.read (extension fields), jobs.read,
-- jobs.write (PR-1029/1031), and search.reindex (PR-1035) — all admin-only
-- in the compiled defaults.
--
-- Why this matters: role_permissions is the actual source of truth once
-- loaded. internal/domain/rbac/effective_permissions.go's HasPermission
-- uses the DB-loaded "effective" store *exclusively* for admin roles once
-- it has been initialized (which happens on every server startup, via
-- adminrole.Service.SyncPluginDefaults -> loadEffectiveLocked ->
-- rbac.InitEffectivePermissions) — it does not fall back to the compiled
-- rolePermissions map for a permission missing from the table.
-- SyncPluginDefaults itself only backfills *plugin-registered*
-- permissions (via EnsurePermissions), not core ones. So every one of
-- these seven permissions has been silently unenforceable for the admin
-- role in any deployment since it was added to the compiled defaults,
-- unless an operator happened to add the matching row by hand (e.g. via
-- the admin roles UI/API).
--
-- ON CONFLICT DO NOTHING: safe to run against a database where an
-- operator already added some of these rows manually.
INSERT INTO role_permissions (role, permission) VALUES
    ('admin', 'customers.store_credit.write'),
    ('admin', 'extensions.read'),
    ('admin', 'extensions.write'),
    ('admin', 'extensions.private.read'),
    ('admin', 'jobs.read'),
    ('admin', 'jobs.write'),
    ('admin', 'search.reindex')
ON CONFLICT DO NOTHING;
