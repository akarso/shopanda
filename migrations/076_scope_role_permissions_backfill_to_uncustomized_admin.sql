-- Code review fix for 075_backfill_role_permissions_core_gaps.sql: that
-- migration unconditionally inserted 7 admin permission rows
-- (customers.store_credit.write, extensions.read/write/private.read,
-- jobs.read/write, search.reindex) that were missing purely due to a
-- historical seeding gap (058_create_role_permissions.sql predates all
-- seven). But an unconditional insert can't tell that apart from an
-- operator having already explicitly configured the admin role's
-- permissions via PUT /api/v1/admin/roles/admin (adminrole.Service.
-- UpdateRole -> RolePermissionRepo.ReplaceForRole, which fully replaces a
-- role's row set) *before* 075 ran, in which case any of these 7 rows
-- being absent reflected that explicit configuration, not the seeding gap
-- — and 075 would have silently restored them, undoing an operator's own
-- prior choice without their knowledge or consent.
--
-- 075 is already shipped (migrations are forward-only; that file is not
-- edited in place — see 074_replace_search_index_runs_started_at_index.sql
-- for the same discipline applied to 073). This migration corrects its
-- effect instead: for each of those 7 (admin, permission) rows, delete it
-- if and only if:
--   1. An admin_audit_log entry recorded a *successful*
--      PUT /api/v1/admin/roles/admin (action='role.update',
--      resource_type='admin_role', resource_id='admin') at or before the
--      moment 075 was applied (schema_migrations.applied_at) — i.e. the
--      admin role's permissions had already been explicitly configured
--      by the time 075 ran, so the row's absence was deliberate, not
--      accidental.
--   2. AND no such successful update happened *after* 075 was applied —
--      if one did, that later save is what actually determines the
--      row now (ReplaceForRole already applied it), and is left alone:
--      whatever the operator's most recent save says is authoritative,
--      whether or not it happens to include one of these 7 permissions.
--
-- When admin's permissions were never explicitly configured at all
-- (the common case — no PUT ever issued for the admin role), neither
-- condition holds and 075's rows are left in place: that's the intended,
-- correct backfill of a compiled default that was never actually
-- reachable due to the historical gap.
DELETE FROM role_permissions rp
USING schema_migrations sm
WHERE rp.role = 'admin'
  AND rp.permission IN (
        'customers.store_credit.write',
        'extensions.read',
        'extensions.write',
        'extensions.private.read',
        'jobs.read',
        'jobs.write',
        'search.reindex'
  )
  AND sm.version = '075_backfill_role_permissions_core_gaps.sql'
  AND EXISTS (
        SELECT 1 FROM admin_audit_log a
        WHERE a.action = 'role.update'
          AND a.resource_type = 'admin_role'
          AND a.resource_id = 'admin'
          AND a.result = 'success'
          AND a.created_at <= sm.applied_at
  )
  AND NOT EXISTS (
        SELECT 1 FROM admin_audit_log a2
        WHERE a2.action = 'role.update'
          AND a2.resource_type = 'admin_role'
          AND a2.resource_id = 'admin'
          AND a2.result = 'success'
          AND a2.created_at > sm.applied_at
  );
