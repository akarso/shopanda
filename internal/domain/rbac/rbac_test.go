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
		rbac.CustomersRead, rbac.CustomersWrite,
		rbac.InvoicesRead,
		rbac.MediaRead, rbac.MediaWrite,
		rbac.SettingsRead, rbac.SettingsWrite,
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
	}
	denied := []rbac.Permission{
		rbac.CustomersWrite,
		rbac.SettingsRead, rbac.SettingsWrite,
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
	}
	denied := []rbac.Permission{
		rbac.OrdersRead, rbac.OrdersWrite,
		rbac.CustomersRead, rbac.CustomersWrite,
		rbac.InvoicesRead,
		rbac.SettingsRead, rbac.SettingsWrite,
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
	}
	denied := []rbac.Permission{
		rbac.ProductsWrite,
		rbac.OrdersWrite,
		rbac.CategoriesRead, rbac.CategoriesWrite,
		rbac.CustomersWrite,
		rbac.MediaRead, rbac.MediaWrite,
		rbac.SettingsRead, rbac.SettingsWrite,
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
	if len(perms) != 13 {
		t.Errorf("admin permissions count = %d, want 13", len(perms))
	}
}

func TestPermissionsForRole_Unknown(t *testing.T) {
	perms := rbac.PermissionsForRole("bogus")
	if perms != nil {
		t.Errorf("unknown role should return nil, got %v", perms)
	}
}

func TestRegisterPluginPermission(t *testing.T) {
	t.Cleanup(rbac.ResetPluginPermissions)

	perm := rbac.Permission("analytics.read")
	err := rbac.RegisterPluginPermission(perm, identity.RoleAdmin, identity.RoleManager)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !rbac.HasPermission(identity.RoleAdmin, perm) {
		t.Error("admin should have plugin permission")
	}
	if !rbac.HasPermission(identity.RoleManager, perm) {
		t.Error("manager should have plugin permission")
	}
	if rbac.HasPermission(identity.RoleEditor, perm) {
		t.Error("editor should not have plugin permission")
	}
}

func TestRegisterPluginPermission_RejectsCoreOverride(t *testing.T) {
	t.Cleanup(rbac.ResetPluginPermissions)

	err := rbac.RegisterPluginPermission(rbac.ProductsRead, identity.RoleSupport)
	if err == nil {
		t.Fatal("expected error when overriding core permission")
	}
}

func TestRegisterPluginPermission_RejectsEmpty(t *testing.T) {
	t.Cleanup(rbac.ResetPluginPermissions)

	err := rbac.RegisterPluginPermission("", identity.RoleAdmin)
	if err == nil {
		t.Fatal("expected error for empty permission")
	}
}

func TestRegisterPluginPermission_RejectsDuplicate(t *testing.T) {
	t.Cleanup(rbac.ResetPluginPermissions)

	perm := rbac.Permission("reports.read")
	if err := rbac.RegisterPluginPermission(perm, identity.RoleAdmin); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	err := rbac.RegisterPluginPermission(perm, identity.RoleManager)
	if err == nil {
		t.Fatal("expected error for duplicate plugin permission")
	}
	// Original grant should be unchanged — admin still has it.
	if !rbac.HasPermission(identity.RoleAdmin, perm) {
		t.Error("original grant should be preserved")
	}
	if rbac.HasPermission(identity.RoleManager, perm) {
		t.Error("duplicate registration should not have taken effect")
	}
}
