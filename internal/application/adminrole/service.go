package adminrole

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/infrastructure/localcache"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// catalogCacheTTL bounds how long a computed Catalog() result is reused
// (PR-1040's L1 tier). permReg is already frozen by the time a Service
// exists (see NewService) and never mutates again for the life of the
// process, so this cache never actually needs invalidating — the TTL
// here is uniform L1 policy ("seconds, not minutes" everywhere L1 is
// used), not a correctness requirement for this specific site.
const catalogCacheTTL = 30 * time.Second

// catalogCacheKey is the sole entry Catalog()'s L1 cache ever holds —
// there is exactly one catalog per process, not one per caller/role.
const catalogCacheKey = "catalog"

// Service manages editable admin role permission assignments.
type Service struct {
	repo    rbac.Repository
	permReg *rbac.Registry
	mu      sync.Mutex

	catalogCache *localcache.Store[[]PermissionCatalogEntry]
}

// NewService creates an admin role service.
// permReg is the same frozen registry used during plugin Init (may be empty).
func NewService(repo rbac.Repository, permReg *rbac.Registry) *Service {
	if repo == nil {
		panic("adminrole.NewService: nil repository")
	}
	if permReg == nil {
		permReg = rbac.NewRegistry()
		permReg.Freeze()
	}
	return &Service{
		repo:         repo,
		permReg:      permReg,
		catalogCache: localcache.New[[]PermissionCatalogEntry](1, catalogCacheTTL),
	}
}

// PermissionCatalogEntry describes an assignable permission.
type PermissionCatalogEntry struct {
	Permission string   `json:"permission"`
	Source     string   `json:"source"`
	Defaults   []string `json:"default_roles,omitempty"`
}

// RolePermissions holds a role and its effective permissions.
type RolePermissions struct {
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

// Catalog returns assignable permissions grouped for the roles editor,
// served from an L1 cache (PR-1040) — see catalogCacheTTL's own doc
// comment for why this is safe. Returns a fresh, deep copy each call so
// a caller mutating any part of its result — not just a top-level field,
// but an element of one entry's own Defaults slice too — can never
// corrupt the cached entry or another caller's own copy.
func (s *Service) Catalog() []PermissionCatalogEntry {
	if cached, ok := s.catalogCache.Get(catalogCacheKey); ok {
		return cloneCatalog(cached)
	}
	computed := s.computeCatalog()
	s.catalogCache.Set(catalogCacheKey, computed)
	return cloneCatalog(computed)
}

// cloneCatalog deep-copies entries, including each entry's own Defaults
// slice — a plain top-level append([]PermissionCatalogEntry(nil), ...)
// would still leave every entry's Defaults aliased to the cached
// original, since a struct copy doesn't copy the slice it points to.
func cloneCatalog(entries []PermissionCatalogEntry) []PermissionCatalogEntry {
	out := make([]PermissionCatalogEntry, len(entries))
	for i, e := range entries {
		out[i] = e
		if e.Defaults != nil {
			out[i].Defaults = append([]string(nil), e.Defaults...)
		}
	}
	return out
}

// computeCatalog is Catalog's own, uncached computation.
func (s *Service) computeCatalog() []PermissionCatalogEntry {
	coreSet := make(map[rbac.Permission]struct{}, len(rbac.CorePermissions()))
	for _, p := range rbac.CorePermissions() {
		coreSet[p] = struct{}{}
	}

	pluginDefaults := make(map[rbac.Permission][]string)
	for _, entry := range s.permReg.PluginPermissions() {
		roles := make([]string, 0, len(entry.DefaultRoles))
		for _, role := range entry.DefaultRoles {
			roles = append(roles, string(role))
		}
		sort.Strings(roles)
		pluginDefaults[entry.Permission] = roles
	}

	out := make([]PermissionCatalogEntry, 0, len(s.permReg.CatalogPermissions()))
	for _, perm := range s.permReg.CatalogPermissions() {
		entry := PermissionCatalogEntry{Permission: string(perm), Source: "core"}
		if _, ok := coreSet[perm]; !ok {
			entry.Source = "plugin"
			entry.Defaults = pluginDefaults[perm]
		}
		out = append(out, entry)
	}
	return out
}

// ListRoles returns all customizable roles with effective permissions.
func (s *Service) ListRoles(ctx context.Context) ([]RolePermissions, error) {
	assignments, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("adminrole: list roles: %w", err)
	}
	out := make([]RolePermissions, 0, len(rbac.AdminRoles()))
	for _, role := range rbac.AdminRoles() {
		out = append(out, toRolePermissions(role, assignments[role]))
	}
	return out, nil
}

// GetRole returns permissions for a single role.
func (s *Service) GetRole(ctx context.Context, role identity.Role) (*RolePermissions, error) {
	if !rbac.IsAdminRole(role) {
		return nil, apperror.Validation("invalid admin role")
	}
	assignments, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("adminrole: get role: %w", err)
	}
	resp := toRolePermissions(role, assignments[role])
	return &resp, nil
}

// UpdateRole replaces permissions for a role and reloads the in-memory RBAC store.
func (s *Service) UpdateRole(ctx context.Context, role identity.Role, rawPermissions []string) (*RolePermissions, error) {
	if !rbac.IsAdminRole(role) {
		return nil, apperror.Validation("invalid admin role")
	}
	perms, err := s.normalizePermissions(rawPermissions)
	if err != nil {
		return nil, err
	}
	if len(perms) == 0 {
		return nil, apperror.Validation("at least one permission is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.repo.ReplaceForRole(ctx, role, perms); err != nil {
		return nil, fmt.Errorf("adminrole: update role: %w", err)
	}
	rbac.SetEffectiveRolePermissions(role, perms)

	resp := toRolePermissions(role, perms)
	return &resp, nil
}

// LoadEffective loads DB assignments into the RBAC runtime store.
func (s *Service) LoadEffective(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadEffectiveLocked(ctx)
}

// SyncPluginDefaults inserts plugin default grants that are not yet persisted.
func (s *Service) SyncPluginDefaults(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, entry := range s.permReg.PluginPermissions() {
		for _, role := range entry.DefaultRoles {
			if err := s.repo.EnsurePermissions(ctx, role, []rbac.Permission{entry.Permission}); err != nil {
				return fmt.Errorf("adminrole: sync plugin defaults: %w", err)
			}
		}
	}
	return s.loadEffectiveLocked(ctx)
}

func (s *Service) loadEffectiveLocked(ctx context.Context) error {
	assignments, err := s.repo.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("adminrole: load effective: %w", err)
	}
	rbac.InitEffectivePermissions(assignments)
	return nil
}

func (s *Service) normalizePermissions(raw []string) ([]rbac.Permission, error) {
	seen := make(map[rbac.Permission]struct{}, len(raw))
	out := make([]rbac.Permission, 0, len(raw))
	for _, item := range raw {
		perm := rbac.Permission(item)
		if perm == "" {
			return nil, apperror.Validation("permission must not be empty")
		}
		if !s.permReg.IsCatalogPermission(perm) {
			return nil, apperror.Validation("unknown permission: " + item)
		}
		if _, ok := seen[perm]; ok {
			continue
		}
		seen[perm] = struct{}{}
		out = append(out, perm)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func toRolePermissions(role identity.Role, perms []rbac.Permission) RolePermissions {
	names := make([]string, 0, len(perms))
	for _, perm := range perms {
		names = append(names, string(perm))
	}
	sort.Strings(names)
	return RolePermissions{
		Role:        string(role),
		Permissions: names,
	}
}
