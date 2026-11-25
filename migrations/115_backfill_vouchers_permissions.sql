-- Backfill role_permissions for vouchers.read / vouchers.write (PR-1803).
-- Same class of gap as downloads (109): compiled defaults are not the
-- source of truth once rbac.InitEffectivePermissions has run.
--
-- Only insert for roles that have never been explicitly customized via
-- PUT /api/v1/admin/roles/{role} (admin_audit_log role.update success).
-- Customized roles keep their operator-chosen set; ReplaceForRole would
-- otherwise drop these rows on the next save if they were forced in.
-- Editor is intentionally omitted (matches downloads).
INSERT INTO role_permissions (role, permission)
SELECT v.role, v.permission
FROM (VALUES
    ('admin', 'vouchers.read'),
    ('admin', 'vouchers.write'),
    ('manager', 'vouchers.read'),
    ('manager', 'vouchers.write'),
    ('support', 'vouchers.read'),
    ('support', 'vouchers.write')
) AS v(role, permission)
WHERE NOT EXISTS (
    SELECT 1 FROM admin_audit_log a
    WHERE a.action = 'role.update'
      AND a.resource_type = 'admin_role'
      AND a.resource_id = v.role
      AND a.result = 'success'
)
ON CONFLICT DO NOTHING;
