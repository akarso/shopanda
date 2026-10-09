package storefront_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
)

func TestPublicProductVisibilityRoutes_Contract(t *testing.T) {
	want := []storefront.ProductVisibilityRoute{
		{Method: "GET", Path: "/api/v1/products", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/products/{id}", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/products/{id}/variants", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/products/{id}/variants/{variantId}", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/products/{id}/reviews", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "POST", Path: "/api/v1/products/{id}/reviews", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/categories/{id}/products", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/search", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/api/v1/search/suggest", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/sitemap.xml", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/products", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/products/{slug}", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/search", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/categories/{slug}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/fragments/search-suggest", Surface: catalog.VisibilitySurfaceSearch, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/fragments/recently-viewed", Surface: catalog.VisibilitySurfaceIndividually, EnforceIn: "PR-1057"},
		{Method: "POST", Path: "/api/v1/carts/{cartId}/items", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "PUT", Path: "/api/v1/carts/{cartId}/items/{variantId}", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "POST", Path: "/api/v1/checkout", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "POST", Path: "/cart/add", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "POST", Path: "/cart/update", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "POST", Path: "/fragments/cart/add", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "POST", Path: "/fragments/cart/update", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "GET", Path: "/api/v1/carts/{cartId}", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/fragments/mini-cart", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
	}

	got := storefront.PublicProductVisibilityRoutes()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}

	knownSurface := map[catalog.VisibilitySurface]bool{
		catalog.VisibilitySurfaceCatalog:      true,
		catalog.VisibilitySurfaceSearch:       true,
		catalog.VisibilitySurfaceIndividually: true,
		catalog.VisibilitySurfacePurchasable:  true,
	}
	seen := make(map[string]struct{}, len(got))
	for i, r := range got {
		w := want[i]
		key := r.Method + " " + r.Path
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate route %s", key)
		}
		seen[key] = struct{}{}
		if r.Method != w.Method || r.Path != w.Path {
			t.Fatalf("[%d] route = %s %s, want %s %s", i, r.Method, r.Path, w.Method, w.Path)
		}
		if r.Surface != w.Surface {
			t.Errorf("%s surface = %q, want %q", key, r.Surface, w.Surface)
		}
		if r.EnforceIn != w.EnforceIn {
			t.Errorf("%s EnforceIn = %q, want %q", key, r.EnforceIn, w.EnforceIn)
		}
		if !knownSurface[r.Surface] {
			t.Errorf("%s unknown surface %q", key, r.Surface)
		}
		switch r.EnforceIn {
		case "PR-1057", "PR-1058", "out-of-scope":
		default:
			t.Errorf("%s EnforceIn = %q, want PR-1057, PR-1058, or out-of-scope", key, r.EnforceIn)
		}
		if r.Note == "" {
			t.Errorf("%s missing Note", key)
		}
	}
}
