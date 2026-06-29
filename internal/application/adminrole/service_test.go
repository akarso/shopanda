package adminrole_test

import (
	"context"
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
	svc := adminroleApp.NewService(repo)

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
	svc := adminroleApp.NewService(repo)

	_, err := svc.UpdateRole(context.Background(), identity.RoleEditor, []string{"not.real"})
	if err == nil {
		t.Fatal("expected unknown permission error")
	}
}

func TestService_Catalog_IncludesCorePermissions(t *testing.T) {
	svc := adminroleApp.NewService(&stubRolePermRepo{})
	catalog := svc.Catalog()
	if len(catalog) < len(rbac.CorePermissions()) {
		t.Fatalf("catalog count = %d, want at least %d", len(catalog), len(rbac.CorePermissions()))
	}
}
