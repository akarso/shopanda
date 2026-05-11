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
	if !strings.Contains(body, "admin-context-store") || !strings.Contains(body, "admin-context-language") || !strings.Contains(body, "admin-context-currency") {
		t.Fatalf("expected global context switcher controls in index html")
	}
	if !strings.Contains(body, "Sales") || !strings.Contains(body, "Catalog") {
		t.Fatalf("expected grouped domain navigation labels in index html")
	}
	if !strings.Contains(body, "/admin/store") || !strings.Contains(body, "/admin/integrations") {
		t.Fatalf("expected store management and integrations links in index html")
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
	if !strings.Contains(body, "admin-context-switcher") {
		t.Fatalf("expected context switcher styles in CSS")
	}
	if !strings.Contains(body, "settings-scope-badge") || !strings.Contains(body, "settings-scope-banner") {
		t.Fatalf("expected settings scope affordance styles in CSS")
	}
	if !strings.Contains(body, "nav-group") {
		t.Fatalf("expected grouped nav styles in CSS")
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
	if !strings.Contains(normalizedBody, "This account has no admin permissions.") {
		t.Fatalf("expected admin role guard message in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have products access.") {
		t.Fatalf("expected products permission error message in JS")
	}
	if !strings.Contains(normalizedBody, "X-Admin-Store-ID") || !strings.Contains(normalizedBody, "X-Admin-Language") || !strings.Contains(normalizedBody, "X-Admin-Currency") {
		t.Fatalf("expected admin scope headers in JS")
	}
	if !strings.Contains(normalizedBody, "field_scopes") || !strings.Contains(normalizedBody, "Store-scoped") || !strings.Contains(normalizedBody, "Current settings scope") {
		t.Fatalf("expected scope metadata settings UX wiring in JS")
	}
	if !strings.Contains(normalizedBody, "renderShippingSettingsPage") || !strings.Contains(normalizedBody, "renderPaymentSettingsPage") {
		t.Fatalf("expected operations settings pages wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Operational configuration has moved") {
		t.Fatalf("expected settings relocation guidance in JS")
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
