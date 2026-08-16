package rbac

import (
	"fmt"
	"sort"
	"sync"

	"github.com/akarso/shopanda/internal/domain/identity"
)

// Registry holds plugin-registered permissions for one process composition root.
// Create empty → plugins Register during Init → Freeze → request path reads only.
type Registry struct {
	mu     sync.RWMutex
	perms  map[Permission]map[identity.Role]struct{}
	frozen bool
}

// NewRegistry returns an empty, writable permission registry.
func NewRegistry() *Registry {
	return &Registry{
		perms: make(map[Permission]map[identity.Role]struct{}),
	}
}

// Register adds a plugin permission and the roles granted it by default.
// Fails when empty, shadows a core permission, already registered, or frozen.
func (r *Registry) Register(perm Permission, roles ...identity.Role) error {
	if r == nil {
		return fmt.Errorf("rbac: permission registry is nil")
	}
	if perm == "" {
		return fmt.Errorf("rbac: permission must not be empty")
	}
	for _, perms := range rolePermissions {
		if _, exists := perms[perm]; exists {
			return fmt.Errorf("rbac: permission %q is a core permission and cannot be overridden", perm)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frozen {
		return fmt.Errorf("rbac: permission registry is frozen")
	}
	if _, exists := r.perms[perm]; exists {
		return fmt.Errorf("rbac: plugin permission %q is already registered", perm)
	}
	m := make(map[identity.Role]struct{}, len(roles))
	for _, role := range roles {
		m[role] = struct{}{}
	}
	r.perms[perm] = m
	return nil
}

// Freeze seals the registry so further Register calls fail.
func (r *Registry) Freeze() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.frozen = true
	r.mu.Unlock()
}

// Has reports whether a plugin-registered permission grants access to role.
func (r *Registry) Has(role identity.Role, perm Permission) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	roles, ok := r.perms[perm]
	if !ok {
		return false
	}
	_, granted := roles[role]
	return granted
}

// PluginPermissions returns registered plugin permissions and their default roles.
func (r *Registry) PluginPermissions() []PluginPermission {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PluginPermission, 0, len(r.perms))
	for perm, roles := range r.perms {
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

// CatalogPermissions returns core plus registered plugin permissions.
func (r *Registry) CatalogPermissions() []Permission {
	core := CorePermissions()
	seen := make(map[Permission]struct{}, len(core)+8)
	out := make([]Permission, 0, len(core)+8)
	for _, p := range core {
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if r != nil {
		r.mu.RLock()
		for p := range r.perms {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
		r.mu.RUnlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// IsCatalogPermission reports whether perm may be assigned through the roles editor.
func (r *Registry) IsCatalogPermission(perm Permission) bool {
	for _, p := range r.CatalogPermissions() {
		if p == perm {
			return true
		}
	}
	return false
}

// DefaultPermissionsForRole returns compiled-in core grants plus plugin defaults.
func (r *Registry) DefaultPermissionsForRole(role identity.Role) []Permission {
	perms, ok := rolePermissions[role]
	if !ok {
		return nil
	}
	out := make([]Permission, 0, len(perms))
	for p := range perms {
		out = append(out, p)
	}
	for _, entry := range r.PluginPermissions() {
		for _, granted := range entry.DefaultRoles {
			if granted == role {
				out = append(out, entry.Permission)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Process-bound registry used by package HasPermission / CatalogPermissions
// after composition-root freeze. Write-once for the process (tests may Unbind).
var (
	runtimeMu       sync.RWMutex
	runtimeRegistry *Registry
)

// BindRuntime installs the frozen registry for package-level auth/catalog helpers.
// Panics if reg is nil or not frozen. Replacing a different already-bound instance
// panics (serve may bind once; callers must UnbindRuntime before rebinding).
func BindRuntime(reg *Registry) {
	if reg == nil {
		panic("rbac: BindRuntime: registry must not be nil")
	}
	reg.mu.RLock()
	frozen := reg.frozen
	reg.mu.RUnlock()
	if !frozen {
		panic("rbac: BindRuntime: registry must be frozen")
	}
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	if runtimeRegistry != nil && runtimeRegistry != reg {
		panic("rbac: BindRuntime: permission registry already bound to a different instance")
	}
	runtimeRegistry = reg
}

// UnbindRuntime clears the process-bound registry.
// Used by tests and by serve setup failure paths after BindRuntime.
func UnbindRuntime() {
	runtimeMu.Lock()
	runtimeRegistry = nil
	runtimeMu.Unlock()
}

// Runtime returns the process-bound registry, if any.
func Runtime() *Registry {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return runtimeRegistry
}
