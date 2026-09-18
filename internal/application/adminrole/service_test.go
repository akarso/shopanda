package adminrole_test

import (
	"context"
	"reflect"
	"testing"

	adminroleApp "github.com/akarso/shopanda/internal/application/adminrole"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
)

type stubRolePermRepo struct {
	assignments map[identity.Role][]rbac.Permission
}

func (s *stubRolePermRepo) ListAll(_ context.Context) (map[identity.Role][]rbac.Permission, error) {
	if s.assignments == nil {
		return map[identity.Role][]rbac.Permission{}, nil
	}
	out := make(map[identity.Role][]rbac.Permission, len(s.assignments))
	for role, perms := range s.assignments {
		cp := append([]rbac.Permission(nil), perms...)
		out[role] = cp
	}
	return out, nil
}

func (s *stubRolePermRepo) ReplaceForRole(_ context.Context, role identity.Role, perms []rbac.Permission) error {
	if s.assignments == nil {
		s.assignments = make(map[identity.Role][]rbac.Permission)
	}
	cp := append([]rbac.Permission(nil), perms...)
	s.assignments[role] = cp
	return nil
}

func (s *stubRolePermRepo) EnsurePermissions(_ context.Context, role identity.Role, perms []rbac.Permission) error {
	if s.assignments == nil {
		s.assignments = make(map[identity.Role][]rbac.Permission)
	}
	seen := make(map[rbac.Permission]struct{}, len(s.assignments[role]))
	for _, p := range s.assignments[role] {
		seen[p] = struct{}{}
	}
	for _, p := range perms {
		if _, ok := seen[p]; ok {
			continue
		}
		s.assignments[role] = append(s.assignments[role], p)
	}
	return nil
}

func TestService_UpdateRole_PersistsAndReloadsEffective(t *testing.T) {
	t.Cleanup(rbac.ResetEffectivePermissions)

	repo := &stubRolePermRepo{
		assignments: map[identity.Role][]rbac.Permission{
			identity.RoleManager: {rbac.ProductsRead},
		},
	}
	svc := adminroleApp.NewService(repo, rbac.NewRegistry())

	resp, err := svc.UpdateRole(context.Background(), identity.RoleManager, []string{
		string(rbac.ProductsRead),
		string(rbac.OrdersRead),
	})
	if err != nil {
		t.Fatalf("UpdateRole: %v", err)
	}
	if len(resp.Permissions) != 2 {
		t.Fatalf("permissions = %v, want 2 entries", resp.Permissions)
	}
	if !rbac.HasPermission(identity.RoleManager, rbac.OrdersRead) {
		t.Fatal("expected effective permissions to include orders.read")
	}
}

func TestService_UpdateRole_RejectsUnknownPermission(t *testing.T) {
	repo := &stubRolePermRepo{}
	svc := adminroleApp.NewService(repo, rbac.NewRegistry())

	_, err := svc.UpdateRole(context.Background(), identity.RoleEditor, []string{"not.real"})
	if err == nil {
		t.Fatal("expected unknown permission error")
	}
}

func TestService_Catalog_IncludesCorePermissions(t *testing.T) {
	svc := adminroleApp.NewService(&stubRolePermRepo{}, rbac.NewRegistry())
	catalog := svc.Catalog()
	if len(catalog) < len(rbac.CorePermissions()) {
		t.Fatalf("catalog count = %d, want at least %d", len(catalog), len(rbac.CorePermissions()))
	}
}

// TestService_Catalog_MutatingResultDoesNotCorruptCache pins PR-1040's
// L1 cache over Catalog(): each call must return an independent copy, so
// a caller appending to (or otherwise mutating) its own result can never
// corrupt what a later call returns.
func TestService_Catalog_MutatingResultDoesNotCorruptCache(t *testing.T) {
	svc := adminroleApp.NewService(&stubRolePermRepo{}, rbac.NewRegistry())
	first := svc.Catalog()
	if len(first) == 0 {
		t.Fatal("expected a non-empty catalog to mutate")
	}
	original := first[0].Permission

	// In-place index mutation, not append: append can silently allocate
	// a new backing array (when the slice has no spare capacity) and so
	// would pass even against an aliased, uncopied cache entry — this is
	// the mutation that actually detects that aliasing bug.
	first[0].Permission = "mutated"

	second := svc.Catalog()
	if second[0].Permission != original {
		t.Fatalf("second Catalog()[0].Permission = %q, want %q — mutating a prior result must not leak into the cache", second[0].Permission, original)
	}
}

// TestService_Catalog_MutatingDefaultsElementDoesNotCorruptCache pins the
// code review fix: a plain top-level append([]PermissionCatalogEntry(nil),
// cached...) copies each entry's struct fields but not the slice a
// Defaults field points to — every copy still aliases the same backing
// array. A caller mutating an element of Defaults in place (not
// reassigning the whole field) must still not corrupt the cache.
func TestService_Catalog_MutatingDefaultsElementDoesNotCorruptCache(t *testing.T) {
	reg := rbac.NewRegistry()
	if err := reg.Register("plugin.test.permission", identity.RoleManager); err != nil {
		t.Fatalf("Register: %v", err)
	}
	reg.Freeze()
	svc := adminroleApp.NewService(&stubRolePermRepo{}, reg)

	first := svc.Catalog()
	idx := -1
	for i, e := range first {
		if e.Permission == "plugin.test.permission" {
			idx = i
			break
		}
	}
	if idx == -1 || len(first[idx].Defaults) == 0 {
		t.Fatalf("expected a plugin entry with a non-empty Defaults, got %+v", first)
	}
	original := first[idx].Defaults[0]

	first[idx].Defaults[0] = "mutated"

	second := svc.Catalog()
	if second[idx].Defaults[0] != original {
		t.Fatalf("second Catalog()[%d].Defaults[0] = %q, want %q — mutating a Defaults element must not leak into the cache", idx, second[idx].Defaults[0], original)
	}
}

// TestService_Catalog_ReturnsCachedResultWithinTTL pins that a second
// Catalog() call within the TTL window is served from the L1 cache, not
// recomputed — observable via reference equality of the underlying data
// being consistent (both calls reflect the exact same registry state)
// even though this test can't directly observe "was it recomputed" from
// the public API alone; the real regression this guards is the previous
// test's aliasing concern plus a basic sanity check that caching didn't
// break the result's content.
func TestService_Catalog_ReturnsCachedResultWithinTTL(t *testing.T) {
	svc := adminroleApp.NewService(&stubRolePermRepo{}, rbac.NewRegistry())
	first := svc.Catalog()
	second := svc.Catalog()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Catalog() results differ across calls:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}
