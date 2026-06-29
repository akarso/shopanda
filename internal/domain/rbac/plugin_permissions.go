package rbac

import (
	"fmt"
	"sort"
	"sync"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// pluginPermissions stores permissions registered by plugins at startup.
// Reads happen at request time; writes happen only during plugin init.
var (
	pluginMu    sync.RWMutex
	pluginPerms = make(map[Permission]map[identity.Role]struct{})
)

// RegisterPluginPermission registers a plugin-defined permission and the
// roles that are granted it. Must be called during plugin initialization.
// Returns an error if the permission is empty or if it shadows a core
// permission that is already defined in rolePermissions.
func RegisterPluginPermission(perm Permission, roles ...identity.Role) error {
	if perm == "" {
		return fmt.Errorf("rbac: permission must not be empty")
	}
	// Reject if the permission is already mapped in the static table for
	// any role, because plugin permissions must not override core ones.
	for _, perms := range rolePermissions {
		if _, exists := perms[perm]; exists {
			return fmt.Errorf("rbac: permission %q is a core permission and cannot be overridden", perm)
		}
	}
	pluginMu.Lock()
	defer pluginMu.Unlock()
	if _, exists := pluginPerms[perm]; exists {
		return fmt.Errorf("rbac: plugin permission %q is already registered", perm)
	}
	m := make(map[identity.Role]struct{}, len(roles))
	for _, r := range roles {
		m[r] = struct{}{}
	}
	pluginPerms[perm] = m
	return nil
}

// hasPluginPermission reports whether a plugin-registered permission
// grants access to the given role.
func hasPluginPermission(role identity.Role, perm Permission) bool {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	roles, ok := pluginPerms[perm]
	if !ok {
		return false
	}
	_, granted := roles[role]
	return granted
}

// ResetPluginPermissions clears all plugin-registered permissions.
// Intended for use in tests only.
func ResetPluginPermissions() {
	pluginMu.Lock()
	pluginPerms = make(map[Permission]map[identity.Role]struct{})
	pluginMu.Unlock()
}

// PluginPermissions returns registered plugin permissions and their default roles.
func PluginPermissions() []PluginPermission {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	out := make([]PluginPermission, 0, len(pluginPerms))
	for perm, roles := range pluginPerms {
		entry := PluginPermission{Permission: perm}
		for role := range roles {
			entry.DefaultRoles = append(entry.DefaultRoles, role)
		}
		sort.Slice(entry.DefaultRoles, func(i, j int) bool {
			return entry.DefaultRoles[i] < entry.DefaultRoles[j]
		})
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Permission < out[j].Permission })
	return out
}

func pluginPermissionSnapshot() map[Permission]map[identity.Role]struct{} {
	pluginMu.RLock()
	defer pluginMu.RUnlock()
	out := make(map[Permission]map[identity.Role]struct{}, len(pluginPerms))
	for perm, roles := range pluginPerms {
		roleCopy := make(map[identity.Role]struct{}, len(roles))
		for role := range roles {
			roleCopy[role] = struct{}{}
		}
		out[perm] = roleCopy
	}
	return out
}
