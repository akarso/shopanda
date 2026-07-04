package http_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
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
	if !strings.Contains(body, "/admin/catalog/prices") || !strings.Contains(body, "Bulk Prices") {
		t.Fatalf("expected bulk prices nav link in index html")
	}
	if !strings.Contains(body, "/admin/catalog/reviews") || !strings.Contains(body, "Reviews") {
		t.Fatalf("expected reviews nav link in index html")
	}
	if !strings.Contains(body, "/admin/marketing/abandoned-cart") || !strings.Contains(body, "Abandoned Cart") {
		t.Fatalf("expected abandoned cart nav link in index html")
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
	if !strings.Contains(body, "admin-tree-list") || !strings.Contains(body, "product-category-picker") {
		t.Fatalf("expected category tree picker styles in CSS")
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
		"renderProductCategoryPickerTree",
		"renderProductCategoryPickerBody",
		"bindProductCategoryPickerActions",
		"filterCategoryTreeForSearch",
		"product-category-picker",
		"product-category-tree",
		"data-product-category-toggle",
		"countAssignedCategories",
		"Category removed from product.",
		"Filter categories",
		"categories') + ' assigned.",
		"categories.write",
		"data-product-category-mutation-busy",
		"aria-busy",
		"aria-live=\"polite\"",
		"role=\"status\"",
		"Saving category assignment...",
		"Category assigned.",
		"aria-label=\"Assign category",
		"clampPagedOffset",
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
	expectedBlockPlacementWiring := []string{
		"renderHomeBlockPlacements",
		"mountBlockPlacementsPanel",
		"contentBlockTargetPath",
		"/admin/content/home-blocks",
		"home-block-placements-btn",
		"page-block-placements",
		"/admin/content-block-targets/",
		"block_ids",
		"block-placement-move-up",
		"block-placement-move-down",
		"block-placement-remove",
		"Failed to load block placements.",
		"Failed to save block placements.",
		"Block placements saved.",
	}
	for _, expected := range expectedBlockPlacementWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected block placement wiring %q in JS", expected)
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
	expectedPromotionHelperWiring := []string{
		"formatPromotionDiscountSummary",
		"detectPromotionTemplate",
		"renderPromotionTierRows",
		"setupPromotionFormInteractions",
		"buildPromotionGuidedPayload",
		"parsePromotionAdvancedPayload",
		"promotion_template",
		"promotion-tiers-editor",
		"promotion-add-tier",
		"promotion-advanced-panel",
		"conditions_json",
		"actions_json",
		"rules_mode",
		"Tiered up to",
		"buy X get Y",
		"Advanced JSON",
		"Sync from guided fields",
	}
	for _, expected := range expectedPromotionHelperWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected promotion helper wiring %q in JS", expected)
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
	expectedWebhookCrudWiring := []string{
		"renderWebhooksGrid",
		"renderWebhookForm",
		"renderWebhookCreate",
		"renderWebhookEdit",
		"/admin/integrations/webhooks",
		"/admin/integrations/webhooks/new",
		"/admin/webhooks/events",
		"new-webhook-btn",
		"delete-webhook-btn",
		"webhook_event",
		"rotate_secret",
		"renderWebhookSecretNotice",
		"Signing secret (copy now — shown once)",
		"Manage webhooks",
		"Failed to load webhooks.",
		"Failed to load webhook form.",
		"Failed to load webhook events.",
		"Webhook endpoint not found.",
		"Failed to delete webhook.",
		"Webhook saved.",
	}
	for _, expected := range expectedWebhookCrudWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected webhook CRUD wiring %q in JS", expected)
		}
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
	expectedStoreCreditWiring := []string{
		"mountCustomerStoreCreditPanel",
		"customer-store-credit",
		"customer-store-credit-issue-form",
		"/store-credit/issue",
		"/store-credit?offset=0&limit=20",
		"Issue credit",
		"Store credit issued.",
		"Failed to load store credit.",
		"Failed to issue store credit.",
		"Your account does not have customer access.",
		"Your account does not have permission to issue store credit.",
		"Recent ledger",
	}
	for _, expected := range expectedStoreCreditWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected store credit wiring %q in JS", expected)
		}
	}
	expectedCustomerGroupsWiring := []string{
		"renderCustomerGroupsGrid",
		"renderCustomerGroupForm",
		"mountCustomerGroupMembersPanel",
		"mountCustomerGroupPanel",
		"refreshCustomerGroupsNavVisibility",
		"probeCustomerGroupsApi",
		"/admin/customer-groups",
		"/admin/customers/groups/new",
		"customer-group-assign-form",
		"Customer group saved.",
		"Customer assigned to group.",
		"Failed to load customer groups.",
		"Failed to load customer group form.",
		"Your account does not have B2B groups access.",
		"Your account does not have permission to manage customer groups.",
		"plugins.b2b",
		"plugins/b2b/README.md#group-prices",
	}
	for _, expected := range expectedCustomerGroupsWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected customer groups wiring %q in JS", expected)
		}
	}
	expectedOrderInvoicesWiring := []string{
		"mountOrderInvoicesPanel",
		"order-invoices-panel",
		"order-invoice-download-btn",
		"downloadInvoicePdf",
		"/admin/orders/",
		"/invoices/",
		"/pdf",
		"No invoice has been issued for this order yet.",
		"Failed to load invoices.",
		"Failed to download invoice PDF.",
		"Your account does not have invoice access.",
		"Download PDF",
	}
	for _, expected := range expectedOrderInvoicesWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected order invoices wiring %q in JS", expected)
		}
	}
	expectedOrderRefundWiring := []string{
		"mountOrderRefundPanel",
		"order-refund-panel",
		"order-refund-btn",
		"findPaymentForOrder",
		"probeRefundRoute",
		"evaluateOrderRefundEligibility",
		"userHasPermission",
		"/admin/orders/",
		"/refund",
		"Issue full refund",
		"Refund issued successfully.",
		"Refund failed.",
		"Only full refunds are supported",
		"Online refunds are only supported for Stripe payments",
		"Your account does not have permission to issue refunds.",
		"Refund API is not available",
	}
	for _, expected := range expectedOrderRefundWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected order refund wiring %q in JS", expected)
		}
	}
	expectedBulkPricesWiring := []string{
		"renderBulkPricesGrid",
		"/admin/catalog/prices",
		"bulk-price-grid",
		"bulk-price-save-btn",
		"bulk-price-scope",
		"renderBulkPriceScopeBadge",
		"applyBulkPriceRowData",
		"loadBulkPriceForRow",
		"Store override",
		"Global fallback",
		"Select a currency context in the header switcher to view and edit prices.",
		"Your account does not have products access.",
		"Failed to load bulk prices.",
		"Save price failed",
	}
	for _, expected := range expectedBulkPricesWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected bulk prices wiring %q in JS", expected)
		}
	}
	expectedReviewsWiring := []string{
		"renderReviewsGrid",
		"/admin/catalog/reviews",
		"setupProductReviewsPanel",
		"product-reviews-panel",
		"moderateReview",
		"review-approve-btn",
		"review-reject-btn",
		"renderReviewStatusBadge",
		"filterReviewsByProduct",
		"adminQueryParam",
		"/admin/reviews",
		`"/" + action`,
		"Visible on storefront",
		"Hidden from storefront",
		"Moderate product reviews",
		"Failed to load reviews.",
		"Failed to load product reviews.",
		"Review approved.",
		"Review rejected.",
		"products.read",
		"products.write",
	}
	for _, expected := range expectedReviewsWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected reviews wiring %q in JS", expected)
		}
	}
	expectedAbandonedCartWiring := []string{
		"renderAbandonedCartSettingsPage",
		"renderAbandonedCartSettingsForm",
		"/admin/marketing/abandoned-cart",
		"/admin/config?group=marketing",
		"marketing.cart_recovery.enabled",
		"marketing.cart_recovery.delay_hours",
		"cart_recovery_enabled",
		"cart_recovery_delay_hours",
		"Send abandoned cart recovery emails",
		"Failed to load abandoned cart settings.",
		"Delay must be between 1 and 720 hours.",
		"cart_recovery",
	}
	for _, expected := range expectedAbandonedCartWiring {
		if !strings.Contains(normalizedBody, expected) {
			t.Fatalf("expected abandoned cart wiring %q in JS", expected)
		}
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

// adminNavPlaceholderAllowlist lists sidebar paths temporarily allowed to use
// renderPlaceholder during admin UI migration. Keep empty once the UI PR ships.
var adminNavPlaceholderAllowlist = map[string]struct{}{}

func TestAdminHandler_SidebarNavNotPlaceholder(t *testing.T) {
	h := newAdminHandler(t)

	indexRec := httptest.NewRecorder()
	indexReq := httptest.NewRequest(http.MethodGet, "/admin", nil)
	h.ServeHTTP(indexRec, indexReq)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d", indexRec.Code, http.StatusOK)
	}

	jsRec := httptest.NewRecorder()
	jsReq := httptest.NewRequest(http.MethodGet, "/admin/admin.js", nil)
	h.ServeHTTP(jsRec, jsReq)
	if jsRec.Code != http.StatusOK {
		t.Fatalf("admin.js status = %d, want %d", jsRec.Code, http.StatusOK)
	}

	jsBody := strings.NewReplacer(`\"`, `"`, `\\'`, `'`).Replace(jsRec.Body.String())
	if strings.Contains(jsBody, "renderPlaceholder") {
		t.Fatal("renderPlaceholder must not remain in admin.js")
	}

	navPaths := extractAdminSidebarNavPaths(indexRec.Body.String())
	if len(navPaths) == 0 {
		t.Fatal("expected sidebar nav paths from index.html, got none")
	}

	for _, path := range navPaths {
		if _, allowed := adminNavPlaceholderAllowlist[path]; allowed {
			continue
		}
		assertAdminRouteNotPlaceholder(t, jsBody, path)
	}
}

func extractAdminSidebarNavPaths(html string) []string {
	start := strings.Index(html, `<aside class="admin-sidebar">`)
	if start == -1 {
		return nil
	}
	rest := html[start:]
	endRel := strings.Index(rest, "</aside>")
	if endRel == -1 {
		return nil
	}
	section := rest[:endRel]

	re := regexp.MustCompile(`href="(/admin[^"]+)"\s+data-link`)
	matches := re.FindAllStringSubmatch(section, -1)
	seen := make(map[string]struct{}, len(matches))
	paths := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := match[1]
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths
}

func assertAdminRouteNotPlaceholder(t *testing.T, jsBody, path string) {
	t.Helper()
	entry, ok := extractJSRouteObject(jsBody, path)
	if !ok {
		t.Fatalf("sidebar path %q has no routes map entry", path)
	}
	if strings.Contains(entry, "renderPlaceholder") {
		t.Fatalf("sidebar path %q routes to renderPlaceholder", path)
	}
	if !strings.Contains(entry, "render:") {
		t.Fatalf("sidebar path %q missing render handler", path)
	}
}

func extractJSRouteObject(jsBody, path string) (string, bool) {
	needle := `"` + path + `": `
	idx := strings.Index(jsBody, needle)
	if idx == -1 {
		return "", false
	}
	rest := jsBody[idx+len(needle):]
	braceRel := strings.Index(rest, "{")
	if braceRel == -1 {
		return "", false
	}
	start := idx + len(needle) + braceRel
	depth := 0
	for i := start; i < len(jsBody); i++ {
		switch jsBody[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return jsBody[start : i+1], true
			}
		}
	}
	return "", false
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
