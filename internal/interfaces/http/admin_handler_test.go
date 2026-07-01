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
		"<th scope=\"col\">SKU</th><th scope=\"col\">Name</th><th scope=\"col\">Weight</th><th scope=\"col\">' + priceHeader + '</th><th scope=\"col\">Action</th>",
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
	if !strings.Contains(normalizedBody, "renderNavigationGrid") || !strings.Contains(normalizedBody, "/admin/menus") {
		t.Fatalf("expected navigation admin surface wiring in JS")
	}
	expectedNavigationCrudWiring := []string{
		"renderNavigationEdit",
		"/admin/content/navigation/",
		"menu-items-editor",
		"add-menu-item-btn",
		"menu-item-remove-btn",
		"Failed to load menus.",
		"Failed to load menu form.",
		"Menu not found.",
		"Menu saved.",
	}
	for _, expected := range expectedNavigationCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected navigation CRUD wiring %q in JS", expected)
		}
	}
	if !strings.Contains(normalizedBody, "renderBlocksGrid") || !strings.Contains(normalizedBody, "/admin/content-blocks?offset=0&limit=50") {
		t.Fatalf("expected content blocks admin surface wiring in JS")
	}
	expectedContentBlockCrudWiring := []string{
		"renderBlockForm",
		"renderBlockCreate",
		"renderBlockEdit",
		"renderBlockConfigFields",
		"readBlockConfigFromForm",
		"/admin/content/blocks/new",
		"new-block-btn",
		"delete-block-btn",
		`value="hero">Hero</option>`,
		`value="rich_text">Rich text</option>`,
		`value="product_carousel">Product carousel</option>`,
		`name="headline"`,
		`name="body"`,
		`name="carousel_title"`,
		`name="product_ids"`,
		"Failed to load content blocks.",
		"Failed to load block form.",
		"Content block not found.",
		"Failed to delete block.",
		"Your account does not have content blocks access.",
		"Block saved.",
	}
	for _, expected := range expectedContentBlockCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected content blocks CRUD wiring %q in JS", expected)
		}
	}
	if !strings.Contains(normalizedBody, "renderCouponsGrid") || !strings.Contains(normalizedBody, "/admin/coupons?offset=0&limit=50") {
		t.Fatalf("expected coupons admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "renderPromotionsGrid") || !strings.Contains(normalizedBody, "/admin/promotions?offset=0&limit=50") {
		t.Fatalf("expected promotions admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "renderAttributesGrid") || !strings.Contains(normalizedBody, "/admin/attributes") {
		t.Fatalf("expected attributes admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have categories access.") || !strings.Contains(normalizedBody, "Failed to load attributes.") {
		t.Fatalf("expected attributes admin error messages in JS")
	}
	expectedAttributeCrudWiring := []string{
		"renderAttributeForm",
		"renderAttributeCreate",
		"renderAttributeEdit",
		"/admin/catalog/attributes/new",
		"new-attribute-btn",
		"delete-attribute-btn",
		"Failed to delete attribute.",
		"Attribute not found.",
		"renderAttributeGroupForm",
		"/admin/catalog/attribute-groups/new",
		"delete-attribute-group-btn",
	}
	for _, expected := range expectedAttributeCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected attributes CRUD wiring %q in JS", expected)
		}
	}
	if !strings.Contains(normalizedBody, "renderInventoryGrid") || !strings.Contains(normalizedBody, "/admin/inventory?offset=0&limit=50") {
		t.Fatalf("expected inventory admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Failed to load inventory.") || !strings.Contains(normalizedBody, "inventory-save-btn") {
		t.Fatalf("expected inventory admin error/adjust wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Failed to load promotions.") {
		t.Fatalf("expected promotions admin error messages in JS")
	}
	expectedPromotionCrudWiring := []string{
		"renderPromotionForm",
		"renderPromotionCreate",
		"renderPromotionEdit",
		"/admin/marketing/promotions/new",
		"new-promotion-btn",
		"delete-promotion-btn",
		"Failed to delete promotion.",
		"Promotion not found.",
	}
	for _, expected := range expectedPromotionCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected promotions CRUD wiring %q in JS", expected)
		}
	}
	if !strings.Contains(normalizedBody, "<select name=\"promotion_id\" required>") {
		t.Fatalf("expected coupon promotion select in JS")
	}
	if !strings.Contains(normalizedBody, "Your account does not have products access.") || !strings.Contains(normalizedBody, "Failed to load coupons.") {
		t.Fatalf("expected coupons admin error messages in JS")
	}
	expectedCouponCrudWiring := []string{
		"renderCouponForm",
		"renderCouponCreate",
		"renderCouponEdit",
		"/admin/marketing/coupons/new",
		"new-coupon-btn",
		"delete-coupon-btn",
		"Failed to delete coupon.",
		"Coupon not found.",
	}
	for _, expected := range expectedCouponCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected coupons CRUD wiring %q in JS", expected)
		}
	}
	if !strings.Contains(normalizedBody, "Your account does not have pages access.") || !strings.Contains(normalizedBody, "Failed to load pages.") {
		t.Fatalf("expected pages admin error messages in JS")
	}
	expectedPageCrudWiring := []string{
		"renderPageForm",
		"renderPageCreate",
		"renderPageEdit",
		"/admin/content/pages/new",
		"new-page-btn",
		"delete-page-btn",
		"Failed to delete page.",
		"Page not found.",
	}
	for _, expected := range expectedPageCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected pages CRUD wiring %q in JS", expected)
		}
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
	if !strings.Contains(normalizedBody, "renderAuditLogPage") || !strings.Contains(normalizedBody, "/admin/audit?offset=0&limit=50") {
		t.Fatalf("expected audit log admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Failed to load audit log.") {
		t.Fatalf("expected audit log admin messages in JS")
	}
	if !strings.Contains(normalizedBody, "Role capabilities reflect the current core RBAC model.") || !strings.Contains(normalizedBody, "Failed to load users and roles.") {
		t.Fatalf("expected users and roles admin messages in JS")
	}
	if !strings.Contains(normalizedBody, "renderIntegrationsPage") || !strings.Contains(normalizedBody, "/admin/config?group=email") || !strings.Contains(normalizedBody, "/admin/config?group=media") || !strings.Contains(normalizedBody, "/admin/config?group=plugins") {
		t.Fatalf("expected integrations admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "/admin/webhooks") || !strings.Contains(normalizedBody, "Outbound Webhooks") {
		t.Fatalf("expected outbound webhooks admin surface wiring in JS")
	}
	if !strings.Contains(normalizedBody, "Plugin Settings") || !strings.Contains(normalizedBody, "integrations-plugin-form") || !strings.Contains(normalizedBody, "Failed to load integrations.") {
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
	expectedScopedCatalogWiring := []string{
		"product-scope-banner",
		"renderProductScopeBanner",
		"renderProductFieldScopeBadge",
		"productFieldScope",
		"Current catalog scope:",
		"collectProductTranslationPayload",
		"saveProductTranslations",
		"/translations",
		"loadVariantPrices",
		"bindVariantPriceSave",
		"setVariantPriceScopeHint",
		"variant-price-scope-hint",
		"global_fallback",
		"price_scope",
		"Variant prices edit the",
		"/price",
		"variant-price-save-btn",
		"Price (minor units, ",
		"Select a currency context to edit price.",
	}
	for _, expected := range expectedScopedCatalogWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected scoped catalog editing wiring in JS to contain %q", expected)
		}
	}
	if !strings.Contains(normalizedBody, "renderShippingSettingsPage") || !strings.Contains(normalizedBody, "renderPaymentSettingsPage") {
		t.Fatalf("expected operations settings pages wiring in JS")
	}
	if !strings.Contains(normalizedBody, "/admin/shipping/zones") || !strings.Contains(normalizedBody, "Failed to load shipping zones.") {
		t.Fatalf("expected shipping zones admin surface wiring in JS")
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
