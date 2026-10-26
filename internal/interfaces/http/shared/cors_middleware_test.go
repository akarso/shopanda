package shared_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/interfaces/http/shared"
)

func TestCORSMiddleware_NoConfigIsNoop(t *testing.T) {
	h := shared.CORSMiddleware(nil, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://store.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected CORS header %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_AllowlistAndPreflight(t *testing.T) {
	h := shared.CORSMiddleware([]string{"https://store.example"}, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://store.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://store.example" {
		t.Fatalf("allow origin = %q", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must be off by default")
	}
	assertVaryOrigin(t, rec)
}

func TestCORSMiddleware_RejectsUnknownOrigin(t *testing.T) {
	nextCalled := false
	h := shared.CORSMiddleware([]string{"https://store.example"}, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unknown origin must not be reflected")
	}
	assertVaryOrigin(t, rec)
	if rec.Code != http.StatusMethodNotAllowed || !nextCalled {
		t.Fatalf("unmatched OPTIONS must reach next (code=%d next=%v)", rec.Code, nextCalled)
	}
}

func TestCORSMiddleware_WildcardNeverSetsCredentials(t *testing.T) {
	h := shared.CORSMiddleware([]string{"*"}, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("* + credentials must not enable CORS, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("credentials must not be set for wildcard")
	}
}

func TestCORSMiddleware_WildcardAnonymous(t *testing.T) {
	h := shared.CORSMiddleware([]string{"*"}, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("allow origin = %q, want *", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("anonymous * must not set credentials")
	}
	if got := rec.Header().Get("Vary"); got != "" {
		t.Fatalf("anonymous * must not Vary Origin, got %q", got)
	}
}

func TestCORSMiddleware_SkipsStorefront(t *testing.T) {
	h := shared.CORSMiddleware([]string{"https://store.example"}, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/storefront/csrf"},
		{http.MethodOptions, "/storefront/csrf"},
		{http.MethodPost, "/storefront/cart/items"},
		{http.MethodPut, "/storefront/cart/items/var-1"},
		{http.MethodDelete, "/storefront/cart/items/var-1"},
		{http.MethodOptions, "/storefront/cart/items"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Origin", "https://store.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s %s must not receive CORS, got %q", tc.method, tc.path, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("%s %s must not receive credentials CORS", tc.method, tc.path)
		}
	}
}

func TestCORSMiddleware_SkipsAdmin(t *testing.T) {
	h := shared.CORSMiddleware([]string{"https://store.example"}, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products", nil)
	req.Header.Set("Origin", "https://store.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("admin must not receive CORS")
	}
}

func TestCORSMiddleware_MissingOriginFallsThrough(t *testing.T) {
	h := shared.CORSMiddleware([]string{"https://store.example"}, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want next handler", rec.Code)
	}
	assertVaryOrigin(t, rec)
}

func TestCORSMiddleware_ExplicitCredentials(t *testing.T) {
	h := shared.CORSMiddleware([]string{"https://store.example"}, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/p1", nil)
	req.Header.Set("Origin", "https://store.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("want credentials on explicit origin")
	}
	assertVaryOrigin(t, rec)
}

func assertVaryOrigin(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	for _, v := range rec.Header().Values("Vary") {
		if v == "Origin" {
			return
		}
	}
	t.Fatalf("want Vary: Origin, got %q", rec.Header().Values("Vary"))
}
