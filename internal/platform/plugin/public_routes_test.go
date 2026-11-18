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

func TestApp_RegisterPublicRoute_NoCache(t *testing.T) {
	app := &plugin.App{}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if err := app.RegisterPublicRoute("GET /api/customizer/session", handler, plugin.WithNoCache()); err != nil {
		t.Fatalf("RegisterPublicRoute: %v", err)
	}
	if err := app.RegisterPublicRoute("GET /api/customizer/catalog", handler); err != nil {
		t.Fatalf("RegisterPublicRoute catalog: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 2 || !routes[0].NoCache || routes[1].NoCache {
		t.Fatalf("routes = %+v", routes)
	}
	prefixes := plugin.NoCachePathPrefixes(routes)
	if len(prefixes) != 1 || prefixes[0] != "/api/customizer/session" {
		t.Fatalf("NoCachePathPrefixes = %#v", prefixes)
	}
}

func TestNoCachePathPrefixes_StripsMethod(t *testing.T) {
	prefixes := plugin.NoCachePathPrefixes([]plugin.PublicRoute{
		{Pattern: "POST /api/v1/graphql", NoCache: true},
		{Pattern: "/extref/design-line", NoCache: true},
		{Pattern: "GET /extref/health", NoCache: false},
	})
	if len(prefixes) != 2 {
		t.Fatalf("prefixes = %#v", prefixes)
	}
	if prefixes[0] != "/api/v1/graphql" || prefixes[1] != "/extref/design-line" {
		t.Fatalf("prefixes = %#v", prefixes)
	}
}

func TestNoCachePathPrefixes_CutsPathParams(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{"GET /api/x/{id}", "/api/x"},
		{"GET /api/x/{id}/y", "/api/x"},
		{"GET /api/x/", "/api/x"},
		{"/api/x/{id}", "/api/x"},
		{"POST /api/v1/graphql", "/api/v1/graphql"},
	}
	for _, tc := range cases {
		prefixes := plugin.NoCachePathPrefixes([]plugin.PublicRoute{
			{Pattern: tc.pattern, NoCache: true},
		})
		if len(prefixes) != 1 || prefixes[0] != tc.want {
			t.Fatalf("%q: prefixes = %#v, want [%q]", tc.pattern, prefixes, tc.want)
		}
	}
}

func TestApp_RegisterPublicRoute_NoCacheRejectsBroadOrEmptyPrefix(t *testing.T) {
	app := &plugin.App{}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	for _, pattern := range []string{
		"GET /{id}",
		"GET /{locale}/health",
		"GET /api/{ver}/products",
		"/{id}/x",
	} {
		err := app.RegisterPublicRoute(pattern, handler, plugin.WithNoCache())
		if err == nil {
			t.Fatalf("%q: expected NoCache registration error", pattern)
		}
	}
	// Same patterns without NoCache still register.
	if err := app.RegisterPublicRoute("GET /{locale}/ok", handler); err != nil {
		t.Fatalf("without NoCache: %v", err)
	}
	// Two-segment static prefix before `{` is accepted.
	if err := app.RegisterPublicRoute("GET /api/x/{id}", handler, plugin.WithNoCache()); err != nil {
		t.Fatalf("accepted pattern: %v", err)
	}
	prefixes := plugin.NoCachePathPrefixes(app.PublicRoutes())
	if len(prefixes) != 1 || prefixes[0] != "/api/x" {
		t.Fatalf("prefixes = %#v", prefixes)
	}
}
