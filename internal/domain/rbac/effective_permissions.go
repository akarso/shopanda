package rbac

import (
	"sort"
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

func effectiveSet(role identity.Role) (map[Permission]struct{}, bool) {
	effectiveMu.RLock()
	defer effectiveMu.RUnlock()
	if effectivePerms == nil {
		return nil, false
	}
	set, ok := effectivePerms[role]
	return set, ok
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
	perms, ok := rolePermissions[role]
	if !ok {
		return nil
	}
	out := make([]Permission, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	for _, entry := range PluginPermissions() {
		for _, r := range entry.DefaultRoles {
			if r == role {
				out = append(out, entry.Permission)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
