package plugin_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/platform/plugin"
)

func TestApp_RegisterPublicRoute(t *testing.T) {
	app := &plugin.App{}
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	if err := app.RegisterPublicRoute("POST /api/v1/graphql", handler); err != nil {
		t.Fatalf("RegisterPublicRoute() error: %v", err)
	}

	routes := app.PublicRoutes()
	if len(routes) != 1 {
		t.Fatalf("PublicRoutes() len = %d, want 1", len(routes))
	}
	if routes[0].Pattern != "POST /api/v1/graphql" {
		t.Fatalf("Pattern = %q", routes[0].Pattern)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", nil)
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("handler was not invoked")
	}
}

func TestApp_RegisterPublicRoute_DuplicatePatternReturnsError(t *testing.T) {
	app := &plugin.App{}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if err := app.RegisterPublicRoute("POST /api/v1/graphql", handler); err != nil {
		t.Fatalf("first RegisterPublicRoute() error: %v", err)
	}
	err := app.RegisterPublicRoute("POST /api/v1/graphql", handler)
	if err == nil {
		t.Fatal("expected error for duplicate public route pattern")
	}
}

func TestApp_PublicRoutes_Empty(t *testing.T) {
	app := &plugin.App{}
	if routes := app.PublicRoutes(); routes != nil {
		t.Fatalf("PublicRoutes() = %v, want nil", routes)
	}
}
