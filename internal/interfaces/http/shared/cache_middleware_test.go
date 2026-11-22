package shared_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/interfaces/http/shared"
	"github.com/akarso/shopanda/internal/platform/auth"
)

func TestCacheControlMiddleware_CacheableGET(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts", "/api/v1/checkout"}
	mw := shared.CacheControlMiddleware(noCachePrefixes)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Cache-Control")
	if got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q, want %q", got, "public, max-age=300")
	}
}

func TestCacheControlMiddleware_CheckoutHTMLNoStore(t *testing.T) {
	mw := shared.CacheControlMiddleware([]string{"/checkout"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/checkout/confirm", nil))
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestCacheControlMiddleware_NoCachePrefixGET(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts", "/api/v1/checkout"}
	mw := shared.CacheControlMiddleware(noCachePrefixes)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/v1/carts/123", "/api/v1/checkout"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(rr, req)

		got := rr.Header().Get("Cache-Control")
		if got != "no-store" {
			t.Errorf("GET %s: Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

func TestCacheControlMiddleware_PluginNoCachePrefix(t *testing.T) {
	// Mirrors wire_routes merging plugin.NoCachePathPrefixes into the denylist.
	noCachePrefixes := []string{"/api/customizer/session"}
	mw := shared.CacheControlMiddleware(noCachePrefixes)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/customizer/session/abc", nil)
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("NoCache path Cache-Control = %q, want no-store", got)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/customizer/catalog", nil)
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("sibling path Cache-Control = %q, want public max-age", got)
	}
}

func TestCacheControlMiddleware_PluginNoCachePrefixAfterParamCut(t *testing.T) {
	// pathPrefixFromPattern("GET /api/x/{id}") → "/api/x"; real request /api/x/123.
	noCachePrefixes := []string{"/api/x"}
	mw := shared.CacheControlMiddleware(noCachePrefixes)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/x/123", nil)
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("/api/x/123 Cache-Control = %q, want no-store", got)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/y/123", nil)
	handler.ServeHTTP(rr, req)
	if got := rr.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("/api/y/123 Cache-Control = %q, want public max-age", got)
	}
}

func TestCacheControlMiddleware_POSTAlwaysNoStore(t *testing.T) {
	mw := shared.CacheControlMiddleware(nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", nil)
	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Cache-Control")
	if got != "no-store" {
		t.Errorf("POST Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestCacheControlMiddleware_HEADCacheable(t *testing.T) {
	mw := shared.CacheControlMiddleware(nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/api/v1/products", nil)
	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Cache-Control")
	if got != "public, max-age=300" {
		t.Errorf("HEAD Cache-Control = %q, want %q", got, "public, max-age=300")
	}
}

func TestCacheControlMiddleware_WriteMethods(t *testing.T) {
	mw := shared.CacheControlMiddleware(nil)

	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		rr := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/products", nil)
		handler.ServeHTTP(rr, req)

		got := rr.Header().Get("Cache-Control")
		if got != "no-store" {
			t.Errorf("%s Cache-Control = %q, want %q", method, got, "no-store")
		}
	}
}

func TestCacheControlMiddleware_StorefrontPrefixNoStore(t *testing.T) {
	mw := shared.CacheControlMiddleware([]string{"/storefront"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{"/storefront/csrf", "/storefront/cart/items"} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		handler.ServeHTTP(rr, req)
		if got := rr.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s: Cache-Control = %q, want no-store", path, got)
		}
	}
}

func TestCacheControlMiddleware_StorefrontCacheable(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts"}
	mw := shared.CacheControlMiddleware(noCachePrefixes)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/products/blue-widget", nil)
	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Cache-Control")
	if got != "public, max-age=300" {
		t.Errorf("Storefront Cache-Control = %q, want %q", got, "public, max-age=300")
	}
}

func TestCacheControlMiddleware_GETWithAuthorizationNoStore(t *testing.T) {
	mw := shared.CacheControlMiddleware(nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	// Simulate authenticated request: AuthMiddleware sets identity in context.
	ctx := auth.WithIdentity(req.Context(), identity.Identity{UserID: "u1", Role: identity.RoleCustomer})
	req = req.WithContext(ctx)

	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Cache-Control")
	if got != "no-store" {
		t.Errorf("authenticated GET Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestCacheControlMiddleware_PrefixBoundaryNoMatch(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts"}
	mw := shared.CacheControlMiddleware(noCachePrefixes)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/carts-foo", nil)
	// Guest context (default when no AuthMiddleware has run).
	ctx := auth.WithIdentity(req.Context(), identity.Guest())
	req = req.WithContext(ctx)

	handler.ServeHTTP(rr, req)

	got := rr.Header().Get("Cache-Control")
	if got != "public, max-age=300" {
		t.Errorf("GET /api/v1/carts-foo Cache-Control = %q, want %q", got, "public, max-age=300")
	}
}
