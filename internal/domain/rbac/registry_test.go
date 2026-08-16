package rbac_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
)

func TestRegistry_Register(t *testing.T) {
	reg := rbac.NewRegistry()
	perm := rbac.Permission("analytics.read")
	if err := reg.Register(perm, identity.RoleAdmin, identity.RoleManager); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	reg.Freeze()
	rbac.BindRuntime(reg)
	t.Cleanup(rbac.UnbindRuntime)

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

func TestRegistry_RejectsCoreOverride(t *testing.T) {
	reg := rbac.NewRegistry()
	if err := reg.Register(rbac.ProductsRead, identity.RoleSupport); err == nil {
		t.Fatal("expected error when overriding core permission")
	}
}

func TestRegistry_RejectsEmpty(t *testing.T) {
	reg := rbac.NewRegistry()
	if err := reg.Register("", identity.RoleAdmin); err == nil {
		t.Fatal("expected error for empty permission")
	}
}

func TestRegistry_RejectsDuplicate(t *testing.T) {
	reg := rbac.NewRegistry()
	perm := rbac.Permission("reports.read")
	if err := reg.Register(perm, identity.RoleAdmin); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}
	if err := reg.Register(perm, identity.RoleManager); err == nil {
		t.Fatal("expected error for duplicate plugin permission")
	}
	reg.Freeze()
	rbac.BindRuntime(reg)
	t.Cleanup(rbac.UnbindRuntime)
	if !rbac.HasPermission(identity.RoleAdmin, perm) {
		t.Error("original grant should be preserved")
	}
	if rbac.HasPermission(identity.RoleManager, perm) {
		t.Error("duplicate registration should not have taken effect")
	}
}

func TestRegistry_RejectsAfterFreeze(t *testing.T) {
	reg := rbac.NewRegistry()
	reg.Freeze()
	if err := reg.Register(rbac.Permission("late.read"), identity.RoleAdmin); err == nil {
		t.Fatal("expected error after freeze")
	}
}

func TestBindRuntime_NilPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil registry")
		}
	}()
	rbac.BindRuntime(nil)
}

func TestBindRuntime_WritablePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for writable registry")
		}
	}()
	rbac.BindRuntime(rbac.NewRegistry())
}

func TestBindRuntime_SameInstanceIdempotent(t *testing.T) {
	reg := rbac.NewRegistry()
	reg.Freeze()
	rbac.BindRuntime(reg)
	t.Cleanup(rbac.UnbindRuntime)
	rbac.BindRuntime(reg) // same pointer is allowed
	if rbac.Runtime() != reg {
		t.Fatal("runtime registry mismatch")
	}
}

func TestBindRuntime_DifferentInstancePanics(t *testing.T) {
	first := rbac.NewRegistry()
	first.Freeze()
	rbac.BindRuntime(first)
	t.Cleanup(rbac.UnbindRuntime)

	second := rbac.NewRegistry()
	second.Freeze()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when binding a different registry")
		}
	}()
	rbac.BindRuntime(second)
}
