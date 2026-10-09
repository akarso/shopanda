package storefront

import "github.com/akarso/shopanda/internal/domain/catalog"

// ProductVisibilityRoute documents which visibility surface a public product
// route must enforce (PR-1056). Admin routes under /api/v1/admin/… are exempt
// via catalog.WithBypassProductVisibility from AdminContextMiddleware.
//
// EnforceIn names the PR that wires the gate (1057 for reads, 1058 for
// purchasability), or "out-of-scope" when product data may appear without an
// axis gate (e.g. already-in-cart display — PR-1058 decides any re-check).
type ProductVisibilityRoute struct {
	Method    string
	Path      string
	Surface   catalog.VisibilitySurface
	EnforceIn string // "PR-1057", "PR-1058", or "out-of-scope"
	Note      string
}

// PublicProductVisibilityRoutes is the audited storefront/public surface map
// (REST + HTML/SSR + fragments). Shared handlers are listed on the public
// path; admin registration of the same handler inherits bypass.
//
// Keep in sync with cmd/api/wire_routes.go — the contract test parses that
// file and fails if a product-exposing public route is missing here or from
// the explicit exclusion list in visibility_boundary_test.go.
func PublicProductVisibilityRoutes() []ProductVisibilityRoute {
	const (
		pr1057     = "PR-1057"
		pr1058     = "PR-1058"
		outOfScope = "out-of-scope"
	)
	return []ProductVisibilityRoute{
		// REST catalog / PDP / search
		{Method: "GET", Path: "/api/v1/products", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "PLP"},
		{Method: "GET", Path: "/api/v1/products/{id}", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "PDP"},
		{Method: "GET", Path: "/api/v1/products/{id}/variants", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "variants under PDP"},
		{Method: "GET", Path: "/api/v1/products/{id}/variants/{variantId}", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "variant detail"},
		{Method: "GET", Path: "/api/v1/products/{id}/reviews", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "list reviews — parent must be PDP-visible"},
		{Method: "POST", Path: "/api/v1/products/{id}/reviews", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "submit review — same individually gate as GET"},
		{Method: "GET", Path: "/api/v1/categories/{id}/products", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "category listing"},
		{Method: "GET", Path: "/api/v1/search", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: pr1057, Note: "full-text search"},
		{Method: "GET", Path: "/api/v1/search/suggest", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: pr1057, Note: "suggest"},
		{Method: "GET", Path: "/api/v1/content-blocks/{targetType}/{targetKey}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "CMS blocks incl. product carousels"},
		{Method: "GET", Path: "/sitemap.xml", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "discovery listing; requesting a PDP URL still needs individually at request time"},

		// HTML / SSR storefront (incl. carousel hosts)
		{Method: "GET", Path: "/{$}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "home — product carousels via content blocks"},
		{Method: "GET", Path: "/pages/{slug}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "CMS page — product carousels via content blocks"},
		{Method: "GET", Path: "/products", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "HTML PLP"},
		{Method: "GET", Path: "/products/{slug}", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "HTML PDP"},
		{Method: "GET", Path: "/search", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: pr1057, Note: "HTML search"},
		{Method: "GET", Path: "/categories", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "HTML category index — searches catalog and renders products"},
		{Method: "GET", Path: "/categories/{slug}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: pr1057, Note: "HTML category PLP"},
		{Method: "GET", Path: "/fragments/search-suggest", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: pr1057, Note: "HTML search suggest fragment"},
		{Method: "GET", Path: "/fragments/recently-viewed", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: pr1057, Note: "recently viewed cards"},

		// Purchasability mutations (deferred to PR-1058)
		{Method: "POST", Path: "/api/v1/carts/{cartId}/items", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "AddItem"},
		{Method: "PUT", Path: "/api/v1/carts/{cartId}/items/{variantId}", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "qty increase — same purchasable gate as AddItem"},
		{Method: "POST", Path: "/api/v1/checkout", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "checkout start — lines must remain purchasable"},
		{Method: "POST", Path: "/cart/add", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "HTML add to cart"},
		{Method: "POST", Path: "/cart/update", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "HTML cart qty update"},
		{Method: "POST", Path: "/fragments/cart/add", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "fragment add to cart"},
		{Method: "POST", Path: "/fragments/cart/update", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "fragment cart qty update"},
		{Method: "POST", Path: "/checkout/confirm", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: pr1058, Note: "HTML confirm calls StartCheckout directly"},

		// Already-in-cart / checkout UI display — no discovery axis gate (1058 may re-check)
		{Method: "GET", Path: "/api/v1/carts/{cartId}", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "cart GET shows line products already in cart"},
		{Method: "GET", Path: "/cart", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "HTML cart page — already-in-cart policy"},
		{Method: "GET", Path: "/fragments/mini-cart", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "mini-cart fragment — already-in-cart policy"},
		{Method: "GET", Path: "/checkout/address", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "checkout step shows cart lines; not a discovery surface"},
		{Method: "GET", Path: "/checkout/shipping", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "checkout step shows cart lines"},
		{Method: "POST", Path: "/checkout/shipping", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "shipping form post; purchasability gated at confirm"},
		{Method: "GET", Path: "/checkout/payment", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "checkout step shows cart lines"},
		{Method: "POST", Path: "/checkout/payment", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "payment form post; purchasability gated at confirm"},
		{Method: "GET", Path: "/checkout/confirm", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: outOfScope, Note: "confirm page GET (POST is the StartCheckout gate)"},
	}
}
