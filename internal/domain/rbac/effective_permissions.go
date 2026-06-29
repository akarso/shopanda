package rbac

import (
	"sync"

	"github.com/akarso/shopanda/internal/domain/identity"
)

var (
	effectiveMu    sync.RWMutex
	effectivePerms map[identity.Role]map[Permission]struct{}
)

// InitEffectivePermissions loads role permission assignments into memory.
// When initialized, HasPermission and PermissionsForRole use this store exclusively.
func InitEffectivePermissions(assignments map[identity.Role][]Permission) {
	m := make(map[identity.Role]map[Permission]struct{}, len(assignments))
	for role, perms := range assignments {
		m[role] = toSet(perms...)
	}
	effectiveMu.Lock()
	effectivePerms = m
	effectiveMu.Unlock()
}

// ResetEffectivePermissions clears the in-memory store. Intended for tests.
func ResetEffectivePermissions() {
	effectiveMu.Lock()
	effectivePerms = nil
	effectiveMu.Unlock()
}

// SetEffectiveRolePermissions updates one role in the effective store.
func SetEffectiveRolePermissions(role identity.Role, perms []Permission) {
	effectiveMu.Lock()
	defer effectiveMu.Unlock()
	if effectivePerms == nil {
		effectivePerms = make(map[identity.Role]map[Permission]struct{})
	}
	effectivePerms[role] = toSet(perms...)
}

// effectiveAccess reports whether the effective store applies to role and its grants.
// When initialized is false, callers should use static fallbacks.
func effectiveAccess(role identity.Role) (set map[Permission]struct{}, initialized bool) {
	effectiveMu.RLock()
	defer effectiveMu.RUnlock()
	if effectivePerms == nil {
		return nil, false
	}
	if !IsAdminRole(role) {
		return nil, false
	}
	set, ok := effectivePerms[role]
	if !ok {
		return map[Permission]struct{}{}, true
	}
	return set, true
}

func staticHasPermission(role identity.Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if ok {
		if _, granted := perms[perm]; granted {
			return true
		}
	}
	return hasPluginPermission(role, perm)
}

func staticPermissionsForRole(role identity.Role) []Permission {
	return DefaultPermissionsForRole(role)
}
