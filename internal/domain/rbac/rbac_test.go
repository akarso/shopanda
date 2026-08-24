package rbac_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
)

func TestHasPermission_Admin(t *testing.T) {
	// Admin has every permission.
	for _, perm := range []rbac.Permission{
		rbac.ProductsRead, rbac.ProductsWrite,
		rbac.OrdersRead, rbac.OrdersWrite,
		rbac.CategoriesRead, rbac.CategoriesWrite,
		rbac.CustomersRead, rbac.CustomersWrite, rbac.StoreCreditWrite,
		rbac.InvoicesRead,
		rbac.MediaRead, rbac.MediaWrite,
		rbac.ContentRead, rbac.ContentWrite,
		rbac.SettingsRead, rbac.SettingsWrite,
		rbac.ShippingRead, rbac.ShippingWrite,
		rbac.AuditRead,
	} {
		if !rbac.HasPermission(identity.RoleAdmin, perm) {
			t.Errorf("admin should have %q", perm)
		}
	}
}

func TestHasPermission_Manager(t *testing.T) {
	// Manager has a subset — no settings.write, no customers.write.
	allowed := []rbac.Permission{
		rbac.ProductsRead, rbac.ProductsWrite,
		rbac.OrdersRead, rbac.OrdersWrite,
		rbac.CategoriesRead, rbac.CategoriesWrite,
		rbac.CustomersRead,
		rbac.InvoicesRead,
		rbac.MediaRead, rbac.MediaWrite,
		rbac.ContentRead,
		rbac.ShippingRead, rbac.ShippingWrite,
	}
	denied := []rbac.Permission{
		rbac.CustomersWrite, rbac.StoreCreditWrite,
		rbac.ContentWrite,
		rbac.SettingsRead, rbac.SettingsWrite,
		rbac.AuditRead,
	}

	for _, perm := range allowed {
		if !rbac.HasPermission(identity.RoleManager, perm) {
			t.Errorf("manager should have %q", perm)
		}
	}
	for _, perm := range denied {
		if rbac.HasPermission(identity.RoleManager, perm) {
			t.Errorf("manager should not have %q", perm)
		}
	}
}

func TestHasPermission_Editor(t *testing.T) {
	allowed := []rbac.Permission{
		rbac.ProductsRead, rbac.ProductsWrite,
		rbac.CategoriesRead, rbac.CategoriesWrite,
		rbac.MediaRead, rbac.MediaWrite,
		rbac.ContentRead, rbac.ContentWrite,
	}
	denied := []rbac.Permission{
		rbac.OrdersRead, rbac.OrdersWrite,
		rbac.CustomersRead, rbac.CustomersWrite, rbac.StoreCreditWrite,
		rbac.InvoicesRead,
		rbac.SettingsRead, rbac.SettingsWrite,
		rbac.AuditRead,
	}

	for _, perm := range allowed {
		if !rbac.HasPermission(identity.RoleEditor, perm) {
			t.Errorf("editor should have %q", perm)
		}
	}
	for _, perm := range denied {
		if rbac.HasPermission(identity.RoleEditor, perm) {
			t.Errorf("editor should not have %q", perm)
		}
	}
}

func TestHasPermission_Support(t *testing.T) {
	allowed := []rbac.Permission{
		rbac.ProductsRead,
		rbac.OrdersRead,
		rbac.CustomersRead,
		rbac.InvoicesRead,
		rbac.ContentRead,
	}
	denied := []rbac.Permission{
		rbac.ProductsWrite,
		rbac.OrdersWrite,
		rbac.CategoriesRead, rbac.CategoriesWrite,
		rbac.CustomersWrite, rbac.StoreCreditWrite,
		rbac.MediaRead, rbac.MediaWrite,
		rbac.ContentWrite,
		rbac.SettingsRead, rbac.SettingsWrite,
		rbac.AuditRead,
	}

	for _, perm := range allowed {
		if !rbac.HasPermission(identity.RoleSupport, perm) {
			t.Errorf("support should have %q", perm)
		}
	}
	for _, perm := range denied {
		if rbac.HasPermission(identity.RoleSupport, perm) {
			t.Errorf("support should not have %q", perm)
		}
	}
}

func TestHasPermission_Guest(t *testing.T) {
	if rbac.HasPermission(identity.RoleGuest, rbac.ProductsRead) {
		t.Error("guest should have no permissions")
	}
}

func TestHasPermission_Customer(t *testing.T) {
	if rbac.HasPermission(identity.RoleCustomer, rbac.ProductsRead) {
		t.Error("customer should have no admin permissions")
	}
}

func TestHasPermission_Unknown(t *testing.T) {
	if rbac.HasPermission("bogus", rbac.ProductsRead) {
		t.Error("unknown role should have no permissions")
	}
}

func TestPermissionsForRole_Admin(t *testing.T) {
	perms := rbac.PermissionsForRole(identity.RoleAdmin)
	// Core admin grants in role_permissions.go (products/orders/categories/customers/
	// store_credit/invoices/media/content/settings/shipping/audit/extensions*).
	const wantCoreAdmin = 22
	if len(perms) != wantCoreAdmin {
		t.Errorf("admin permissions count = %d, want %d (%v)", len(perms), wantCoreAdmin, perms)
	}
}

// TestContentPermissions_ScopedByRole documents that content authors (Admin,
// Editor) may mutate pages while read-only roles (Manager, Support) can list
// but not mutate them — the PR-396 permission contract.
func TestContentPermissions_ScopedByRole(t *testing.T) {
	cases := []struct {
		role     identity.Role
		canRead  bool
		canWrite bool
	}{
		{identity.RoleAdmin, true, true},
		{identity.RoleEditor, true, true},
		{identity.RoleManager, true, false},
		{identity.RoleSupport, true, false},
		{identity.RoleCustomer, false, false},
		{identity.RoleGuest, false, false},
	}
	for _, c := range cases {
		if got := rbac.HasPermission(c.role, rbac.ContentRead); got != c.canRead {
			t.Errorf("%s content.read = %v, want %v", c.role, got, c.canRead)
		}
		if got := rbac.HasPermission(c.role, rbac.ContentWrite); got != c.canWrite {
			t.Errorf("%s content.write = %v, want %v", c.role, got, c.canWrite)
		}
	}
}

func TestPermissionsForRole_Unknown(t *testing.T) {
	perms := rbac.PermissionsForRole("bogus")
	if perms != nil {
		t.Errorf("unknown role should return nil, got %v", perms)
	}
}

func TestInitEffectivePermissions_OverridesStaticDefaults(t *testing.T) {
	t.Cleanup(rbac.ResetEffectivePermissions)

	rbac.InitEffectivePermissions(map[identity.Role][]rbac.Permission{
		identity.RoleSupport: {rbac.ProductsWrite},
	})

	if !rbac.HasPermission(identity.RoleSupport, rbac.ProductsWrite) {
		t.Fatal("expected customized support role to grant products.write")
	}
	if rbac.HasPermission(identity.RoleSupport, rbac.ProductsRead) {
		t.Fatal("expected customized support role to drop default products.read")
	}

	perms := rbac.PermissionsForRole(identity.RoleSupport)
	if len(perms) != 1 || perms[0] != rbac.ProductsWrite {
		t.Fatalf("PermissionsForRole = %v, want [products.write]", perms)
	}
}

func TestInitEffectivePermissions_MissingAdminRoleHasNoGrants(t *testing.T) {
	t.Cleanup(rbac.ResetEffectivePermissions)

	rbac.InitEffectivePermissions(map[identity.Role][]rbac.Permission{
		identity.RoleAdmin: {rbac.SettingsRead},
	})

	if rbac.HasPermission(identity.RoleManager, rbac.ProductsRead) {
		t.Fatal("expected missing manager role to have no grants")
	}
	if perms := rbac.PermissionsForRole(identity.RoleManager); len(perms) != 0 {
		t.Fatalf("PermissionsForRole(manager) = %v, want empty", perms)
	}
	if perms := rbac.PermissionsForRole("bogus"); perms != nil {
		t.Fatalf("PermissionsForRole(bogus) = %v, want nil", perms)
	}
}
