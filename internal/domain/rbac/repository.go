package rbac

import (
	"context"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// Repository persists editable role permission assignments.
type Repository interface {
	// ListAll returns permissions grouped by admin role.
	ListAll(ctx context.Context) (map[identity.Role][]Permission, error)
	// ReplaceForRole replaces all permissions for a role.
	ReplaceForRole(ctx context.Context, role identity.Role, perms []Permission) error
	// EnsurePermissions inserts missing role/permission pairs without removing existing rows.
	EnsurePermissions(ctx context.Context, role identity.Role, perms []Permission) error
}
