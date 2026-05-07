package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func newAdminHandler(t *testing.T) *shophttp.AdminHandler {
	t.Helper()
	h, err := shophttp.NewAdminHandler()
	if err != nil {
		t.Fatalf("NewAdminHandler: %v", err)
	}
	return h
}

func TestAdminHandler_IndexHTML(t *testing.T) {
	h := newAdminHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("expected text/html, got %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "admin-layout") {
		t.Fatalf("expected admin-layout in body")
	}
	if !strings.Contains(body, "/admin/account") {
		t.Fatalf("expected admin account navigation in index html")
	}
}

func TestAdminHandler_StaticCSS(t *testing.T) {
	h := newAdminHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/admin.css", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "css") {
		t.Fatalf("expected css content-type, got %s", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "admin-sidebar") {
		t.Fatalf("expected admin-sidebar in CSS")
	}
}

func TestAdminHandler_StaticJS(t *testing.T) {
	h := newAdminHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	normalizedBody := strings.NewReplacer(`\"`, `"`, `\\'`, `'`).Replace(body)
	if !strings.Contains(body, "TOKEN_KEY") {
		t.Fatalf("expected TOKEN_KEY in JS")
	}
	if !strings.Contains(normalizedBody, "default credentials") {
		t.Fatalf("expected login credential guidance in JS")
	}
	if !strings.Contains(normalizedBody, "{currency}") || !strings.Contains(normalizedBody, "{amount}") {
		t.Fatalf("expected currency display format hint in JS")
	}
	if strings.Contains(normalizedBody, "admin@example.com") {
		t.Fatalf("expected no hardcoded seeded admin email in JS")
	}
	if !strings.Contains(normalizedBody, `autocomplete="off"`) && !strings.Contains(normalizedBody, "autocomplete='off'") && !strings.Contains(normalizedBody, "autocomplete=off") {
		t.Fatalf("expected admin login form to disable autofill in JS")
	}
	if !strings.Contains(normalizedBody, "/admin/account") {
		t.Fatalf("expected admin account route in JS")
	}
	if !strings.Contains(normalizedBody, "/auth/me/profile") || !strings.Contains(normalizedBody, "/auth/me/password") {
		t.Fatalf("expected admin account API wiring in JS")
	}
}

func TestAdminHandler_SPAFallback(t *testing.T) {
	h := newAdminHandler(t)

	// Unknown sub-path should serve index.html (SPA fallback).
	paths := []string{"/admin/dashboard", "/admin/products", "/admin/orders", "/admin/settings"}
	for _, p := range paths {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", p, rec.Code)
		}
		ct := rec.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/html") {
			t.Fatalf("%s: expected text/html, got %s", p, ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "admin-layout") {
			t.Fatalf("%s: expected admin-layout in body", p)
		}
	}
}

func TestAdminHandler_TrailingSlash(t *testing.T) {
	h := newAdminHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "admin-layout") {
		t.Fatalf("expected admin-layout in body")
	}
}
