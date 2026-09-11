-- Code review fix for 076_scope_role_permissions_backfill_to_uncustomized_
-- admin.sql: that migration deleted all 7 of 075's backfilled (admin,
-- permission) rows whenever an admin_audit_log entry showed the admin
-- role had been explicitly updated before 075 ran (and not since) —
-- reasoning that such an update meant the row's absence was a deliberate
-- choice, not the historical seeding gap 075 was meant to fix.
--
-- That guard is unsound: adminrole.Service.UpdateRole's audit entry
-- (action='role.update') has only ever recorded a permission *count*
-- (see internal/interfaces/http/admin/admin_role.go), never which
-- permissions were actually granted. A pre-075 update could have
-- deliberately retained some of these 7 permissions while omitting
-- others — but 076 could not tell that apart from having omitted all 7,
-- so it deleted all 7 unconditionally whenever its guard fired, silently
-- revoking whichever ones a real update had actually kept. Affected
-- administrators would then hit authorization failures for a permission
-- they were actively, legitimately using.
--
-- There is no way to reconstruct, after the fact, exactly which of these
-- 7 permissions a historical pre-075 update granted: no other table
-- records role_permissions history, and enriching the audit format now
-- cannot retroactively add detail to an update that already happened
-- under the older code that only recorded a count. Absent that data,
-- this migration accepts the same tradeoff 075 made and 076 tried
-- (incompletely) to avoid: between silently restoring a permission an
-- operator may have deliberately revoked (matches what the compiled
-- defaults already say the role should have — a passive, low-severity
-- discrepancy) and silently revoking a permission an operator deliberately
-- kept (an active, disruptive authorization failure for a working admin),
-- the former is the lesser harm. So 076's guard-fired deletions are
-- reversed here: restore any of the 7 permissions currently missing for
-- 'admin' where 076's own exact criteria hold (076 is not edited in
-- place — see 074/076 for the same discipline applied to 073/075).
-- A genuinely precise fix requires an operator to explicitly re-save the
-- admin role's permissions via PUT /api/v1/admin/roles/admin with full
-- knowledge of what it should contain, not a migration guessing from a
-- bare count.
INSERT INTO role_permissions (role, permission)
SELECT 'admin', v.permission
FROM (VALUES
    ('customers.store_credit.write'),
    ('extensions.read'),
    ('extensions.write'),
    ('extensions.private.read'),
    ('jobs.read'),
    ('jobs.write'),
    ('search.reindex')
) AS v(permission)
WHERE EXISTS (
        SELECT 1 FROM schema_migrations sm
        JOIN admin_audit_log a
          ON a.action = 'role.update'
         AND a.resource_type = 'admin_role'
         AND a.resource_id = 'admin'
         AND a.result = 'success'
         AND a.created_at <= sm.applied_at
        WHERE sm.version = '075_backfill_role_permissions_core_gaps.sql'
      )
  AND NOT EXISTS (
        SELECT 1 FROM schema_migrations sm
        JOIN admin_audit_log a2
          ON a2.action = 'role.update'
         AND a2.resource_type = 'admin_role'
         AND a2.resource_id = 'admin'
         AND a2.result = 'success'
         AND a2.created_at > sm.applied_at
        WHERE sm.version = '075_backfill_role_permissions_core_gaps.sql'
      )
ON CONFLICT DO NOTHING;
