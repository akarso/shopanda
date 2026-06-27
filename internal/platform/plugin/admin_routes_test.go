package plugin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_RegisterAdminRoute(t *testing.T) {
	app := &plugin.App{}
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	if err := app.RegisterAdminRoute("GET /api/v1/admin/example", rbac.Permission("example.read"), handler); err != nil {
		t.Fatalf("RegisterAdminRoute() error: %v", err)
	}

	routes := app.AdminRoutes()
	if len(routes) != 1 {
		t.Fatalf("AdminRoutes() len = %d, want 1", len(routes))
	}
	if routes[0].Pattern != "GET /api/v1/admin/example" {
		t.Fatalf("Pattern = %q", routes[0].Pattern)
	}
	if routes[0].Permission != rbac.Permission("example.read") {
		t.Fatalf("Permission = %q", routes[0].Permission)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/example", nil)
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("handler was not invoked")
	}
}

func TestApp_RegisterAdminRoute_EmptyPatternPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty pattern")
		}
	}()
	app := &plugin.App{}
	app.RegisterAdminRoute("", rbac.Permission("x"), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}

func TestApp_RegisterAdminRoute_NilHandlerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil handler")
		}
	}()
	app := &plugin.App{}
	app.RegisterAdminRoute("GET /x", rbac.Permission("x"), nil)
}

func TestApp_RegisterAdminRoute_EmptyPermissionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty permission")
		}
	}()
	app := &plugin.App{}
	app.RegisterAdminRoute("GET /x", "", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}

func TestApp_RegisterAdminRoute_DuplicatePatternReturnsError(t *testing.T) {
	app := &plugin.App{}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if err := app.RegisterAdminRoute("GET /api/v1/admin/example", rbac.Permission("example.read"), handler); err != nil {
		t.Fatalf("first RegisterAdminRoute() error: %v", err)
	}
	err := app.RegisterAdminRoute("GET /api/v1/admin/example", rbac.Permission("example.write"), handler)
	if err == nil {
		t.Fatal("expected error for duplicate admin route pattern")
	}
}

func TestApp_AdminRoutes_Empty(t *testing.T) {
	app := &plugin.App{}
	if routes := app.AdminRoutes(); routes != nil {
		t.Fatalf("AdminRoutes() = %v, want nil", routes)
	}
}
