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
	expectedProductAssignmentWiring := []string{
		"/admin/products/",
		"Assign Category",
		"Category removed from product.",
		"Filter categories",
		"Assigned categories page",
		"Available categories page",
		"Previous Available Categories Page",
		"Next Available Categories Page",
		"Previous Assigned Categories Page",
		"Next Assigned Categories Page",
		"option.slug",
		"category.slug || category.id",
		"assigned.slug || assigned.id",
		"availableCategories.splice",
		"insertCategoryByOrder(assignedCategories, movedCategory, categoryOrderLookup)",
		"insertCategoryByOrder(availableCategories, movedCategory, categoryOrderLookup)",
		"String(existing.id || '') === categoryID",
		"data-product-category-mutation-busy",
		"Assigned products page ' + esc(String(assignedPageNumber)) + ' of ' + esc(String(totalAssignedPages))",
		"aria-busy",
		"aria-live=\"polite\"",
		"role=\"status\"",
		"Saving category assignment...",
		"setMutationBusy(true)",
		"setMutationBusy(false)",
		"rerender(assignedCategories, availableCategories)",
		"reload()",
		"clampPagedOffset",
		"totalAssignedPages",
		"totalAvailablePages",
		"aria-label=\"Remove product",
		"aria-label=\"Remove category",
		"<th scope=\"col\">SKU</th><th scope=\"col\">Name</th><th scope=\"col\">Weight</th><th scope=\"col\">Action</th>",
		"<th scope=\"col\">ID</th><th scope=\"col\">Customer</th><th scope=\"col\">Total</th><th scope=\"col\">Status</th><th scope=\"col\">Payment</th><th scope=\"col\">Date</th><th scope=\"col\">Action</th>",
		"aria-label=\"Save variant",
	}
	for _, expected := range expectedProductAssignmentWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected product detail assignment wiring in JS normalizedBody to contain %q", expected)
		}
	}
	if !strings.Contains(normalizedBody, "renderCategoriesPage") || !strings.Contains(normalizedBody, "/admin/catalog/categories") || !strings.Contains(normalizedBody, "/admin/categories") {
		t.Fatalf("expected categories admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "renderCategoryForm") || !strings.Contains(normalizedBody, "/admin/categories/new") || !strings.Contains(normalizedBody, "Delete Category") || !strings.Contains(normalizedBody, "Move up") || !strings.Contains(normalizedBody, "Move down") || !strings.Contains(normalizedBody, "aria-label=\"Move category") || !strings.Contains(normalizedBody, "aria-label=\"Delete category") {
		t.Fatalf("expected category CRUD routing in JS")
	}
	if !strings.Contains(normalizedBody, "renderProductsGrid") || !strings.Contains(normalizedBody, "renderVariants") || !strings.Contains(normalizedBody, "renderOrdersGrid") {
		t.Fatalf("expected product, variant, and orders admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "/admin/categories/") || !strings.Contains(normalizedBody, "/admin/products?offset=") || !strings.Contains(normalizedBody, "Assign Product") || !strings.Contains(normalizedBody, "Product removed from category.") || !strings.Contains(normalizedBody, "Previous Product Page") || !strings.Contains(normalizedBody, "Next Product Page") || !strings.Contains(normalizedBody, "Previous Assigned Page") || !strings.Contains(normalizedBody, "Next Assigned Page") || !strings.Contains(normalizedBody, "Assigned products page") || !strings.Contains(normalizedBody, "loadAllAssignedProducts") || !strings.Contains(normalizedBody, "Filter products") {
		t.Fatalf("expected category product assignment wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have categories access.") || !strings.Contains(normalizedBody, "Failed to load categories.") || !strings.Contains(normalizedBody, "Failed to load category form.") || !strings.Contains(normalizedBody, "Category meta must be a JSON object.") || !strings.Contains(normalizedBody, "Category order saved.") || !strings.Contains(normalizedBody, "Failed to save category order.") || !strings.Contains(normalizedBody, "Failed to load assigned products.") || !strings.Contains(normalizedBody, "Failed to assign product.") || !strings.Contains(normalizedBody, "Failed to remove product.") || !strings.Contains(normalizedBody, "Your account does not have products access, so product assignment is unavailable.") || !strings.Contains(normalizedBody, "Failed to load assigned categories.") || !strings.Contains(normalizedBody, "Failed to assign category.") || !strings.Contains(normalizedBody, "Failed to remove category.") || !strings.Contains(normalizedBody, "Your account does not have categories access, so category assignment is unavailable.") {
		t.Fatalf("expected categories admin messages in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have customer access.") {
		t.Fatalf("expected customer permission error message in JS")
	}
	if !strings.Contains(normalizedBody, "renderPagesGrid") || !strings.Contains(normalizedBody, "/admin/pages?offset=0&limit=50") {
		t.Fatalf("expected pages admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have pages access.") || !strings.Contains(normalizedBody, "Failed to load pages.") {
		t.Fatalf("expected pages admin error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderStoresGrid") || !strings.Contains(normalizedBody, "/admin/stores") {
		t.Fatalf("expected stores admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have stores access.") || !strings.Contains(normalizedBody, "Failed to load stores.") {
		t.Fatalf("expected stores admin error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderStoreDomainsPage") || !strings.Contains(normalizedBody, "/admin/store/domains") {
		t.Fatalf("expected store domains admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have stores access.") || !strings.Contains(normalizedBody, "Failed to load store domains.") {
		t.Fatalf("expected store domains admin error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderStoreLanguagesPage") || !strings.Contains(normalizedBody, "/admin/store/languages") {
		t.Fatalf("expected store languages admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have stores access.") || !strings.Contains(normalizedBody, "Failed to load store languages.") {
		t.Fatalf("expected store languages admin error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderStoreCurrenciesPage") || !strings.Contains(normalizedBody, "/admin/store/currencies") {
		t.Fatalf("expected store currencies admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have stores access.") || !strings.Contains(normalizedBody, "Failed to load store currencies.") {
		t.Fatalf("expected store currencies admin error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderUsersRolesPage") || !strings.Contains(normalizedBody, "/admin/customers?offset=0&limit=50") {
		t.Fatalf("expected users and roles admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Role capabilities reflect the current core RBAC model.") || !strings.Contains(normalizedBody, "Failed to load users and roles.") {
		t.Fatalf("expected users and roles admin messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderIntegrationsPage") || !strings.Contains(normalizedBody, "/admin/config?group=email") || !strings.Contains(normalizedBody, "/admin/config?group=media") {
		t.Fatalf("expected integrations admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Plugin integrations are registered at application boot.") || !strings.Contains(normalizedBody, "Failed to load integrations.") {
		t.Fatalf("expected integrations admin messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderLocalizationSettingsPage") || !strings.Contains(normalizedBody, "/admin/config?group=currency") {
		t.Fatalf("expected localization admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have settings access.") || !strings.Contains(normalizedBody, "Failed to load localization settings.") {
		t.Fatalf("expected localization admin error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderCustomerDetail") || !strings.Contains(normalizedBody, "/admin/customers/") {
		t.Fatalf("expected customer detail route wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Customer not found.") || !strings.Contains(normalizedBody, "Failed to load customer.") {
		t.Fatalf("expected customer detail error messages in JS")
	}
	if !strings.Contains(normalizedBody, "shopanda_admin_login_message") {
		t.Fatalf("expected admin login flash storage key in JS")
	}
	if !strings.Contains(normalizedBody, "Your session expired. Sign in again to continue.") {
		t.Fatalf("expected admin reauthentication flash message in JS")
	}
	if !strings.Contains(normalizedBody, "Failed to load product grid.") || !strings.Contains(normalizedBody, "Failed to load products.") {
		t.Fatalf("expected granular products loading error messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderCustomersGrid") || !strings.Contains(normalizedBody, "/admin/customers?offset=0&limit=50") || !strings.Contains(normalizedBody, "Failed to load customers.") {
		t.Fatalf("expected customers admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "X-Admin-Store-ID") || !strings.Contains(normalizedBody, "X-Admin-Language") || !strings.Contains(normalizedBody, "X-Admin-Currency") {
		t.Fatalf("expected admin scope headers in JS")
	}
	if !strings.Contains(normalizedBody, "field_scopes") || !strings.Contains(normalizedBody, "Store-scoped") || !strings.Contains(normalizedBody, "Current settings scope") {
		t.Fatalf("expected scope metadata settings UX wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Language: <strong>") || !strings.Contains(normalizedBody, "Currency: <strong>") {
		t.Fatalf("expected scope banner to include language and currency context in JS")
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
