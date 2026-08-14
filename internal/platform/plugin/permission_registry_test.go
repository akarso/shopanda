package plugin_test

import (
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_SetPermissionRegistry_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil registry")
		}
	}()
	app := &plugin.App{}
	app.SetPermissionRegistry(nil)
}

func TestApp_SetPermissionRegistry_DifferentInstancePanics(t *testing.T) {
	app := &plugin.App{}
	app.SetPermissionRegistry(rbac.NewRegistry())
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when replacing with a different registry")
		}
	}()
	app.SetPermissionRegistry(rbac.NewRegistry())
}

func TestApp_SetPermissionRegistry_SameInstanceIdempotent(t *testing.T) {
	app := &plugin.App{}
	reg := rbac.NewRegistry()
	app.SetPermissionRegistry(reg)
	app.SetPermissionRegistry(reg)
	if app.PermissionRegistry() != reg {
		t.Fatal("PermissionRegistry mismatch")
	}
}

func TestApp_RegisterPermission_RequiresRegistry(t *testing.T) {
	app := &plugin.App{}
	err := app.RegisterPermission(rbac.Permission("demo.read"), identity.RoleAdmin)
	if err == nil {
		t.Fatal("expected error when registry not configured")
	}
	if !strings.Contains(err.Error(), "permission registry not configured") {
		t.Fatalf("error = %v, want registry-not-configured message", err)
	}
}

func TestApp_RegisterPermission_AndFreeze(t *testing.T) {
	app := &plugin.App{}
	reg := rbac.NewRegistry()
	app.SetPermissionRegistry(reg)

	perm := rbac.Permission("demo.read")
	if err := app.RegisterPermission(perm, identity.RoleAdmin); err != nil {
		t.Fatalf("RegisterPermission: %v", err)
	}
	app.FreezePermissionRegistry()
	if err := app.RegisterPermission(rbac.Permission("demo.write"), identity.RoleAdmin); err == nil {
		t.Fatal("expected error after freeze")
	}
	if !reg.Has(identity.RoleAdmin, perm) {
		t.Fatal("expected registered permission on app registry")
	}
}
