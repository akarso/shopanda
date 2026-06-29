package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
)

var _ rbac.Repository = (*RolePermissionRepo)(nil)

// RolePermissionRepo implements rbac.Repository using PostgreSQL.
type RolePermissionRepo struct {
	db *sql.DB
}

// NewRolePermissionRepo returns a RolePermissionRepo backed by db.
func NewRolePermissionRepo(db *sql.DB) (*RolePermissionRepo, error) {
	if db == nil {
		return nil, fmt.Errorf("NewRolePermissionRepo: nil *sql.DB")
	}
	return &RolePermissionRepo{db: db}, nil
}

// ListAll returns permissions grouped by admin role.
func (r *RolePermissionRepo) ListAll(ctx context.Context) (map[identity.Role][]rbac.Permission, error) {
	const q = `SELECT role, permission FROM role_permissions ORDER BY role, permission`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("role_permission_repo: list all: %w", err)
	}
	defer rows.Close()

	out := make(map[identity.Role][]rbac.Permission)
	for rows.Next() {
		var roleName, permName string
		if err := rows.Scan(&roleName, &permName); err != nil {
			return nil, fmt.Errorf("role_permission_repo: scan: %w", err)
		}
		role := identity.Role(roleName)
		out[role] = append(out[role], rbac.Permission(permName))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("role_permission_repo: rows: %w", err)
	}
	return out, nil
}

// ReplaceForRole replaces all permissions for a role.
func (r *RolePermissionRepo) ReplaceForRole(ctx context.Context, role identity.Role, perms []rbac.Permission) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("role_permission_repo: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role = $1`, string(role)); err != nil {
		return fmt.Errorf("role_permission_repo: delete role permissions: %w", err)
	}

	const insertQ = `INSERT INTO role_permissions (role, permission) VALUES ($1, $2)`
	for _, perm := range perms {
		if _, err := tx.ExecContext(ctx, insertQ, string(role), string(perm)); err != nil {
			return fmt.Errorf("role_permission_repo: insert permission: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("role_permission_repo: commit: %w", err)
	}
	return nil
}

// EnsurePermissions inserts missing role/permission pairs.
func (r *RolePermissionRepo) EnsurePermissions(ctx context.Context, role identity.Role, perms []rbac.Permission) error {
	const q = `INSERT INTO role_permissions (role, permission) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	for _, perm := range perms {
		if _, err := r.db.ExecContext(ctx, q, string(role), string(perm)); err != nil {
			return fmt.Errorf("role_permission_repo: ensure permission: %w", err)
		}
	}
	return nil
}
