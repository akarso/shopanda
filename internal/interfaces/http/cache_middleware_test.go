package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth"
)

func TestCacheControlMiddleware_CacheableGET(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts", "/api/v1/checkout"}
	mw := shophttp.CacheControlMiddleware(noCachePrefixes)
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

func TestCacheControlMiddleware_NoCachePrefixGET(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts", "/api/v1/checkout"}
	mw := shophttp.CacheControlMiddleware(noCachePrefixes)
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

func TestCacheControlMiddleware_POSTAlwaysNoStore(t *testing.T) {
	mw := shophttp.CacheControlMiddleware(nil)
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
	mw := shophttp.CacheControlMiddleware(nil)
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
	mw := shophttp.CacheControlMiddleware(nil)

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

func TestCacheControlMiddleware_StorefrontCacheable(t *testing.T) {
	noCachePrefixes := []string{"/api/v1/carts"}
	mw := shophttp.CacheControlMiddleware(noCachePrefixes)
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
	mw := shophttp.CacheControlMiddleware(nil)
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
	mw := shophttp.CacheControlMiddleware(noCachePrefixes)
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
