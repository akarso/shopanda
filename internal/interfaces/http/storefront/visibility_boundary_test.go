package storefront_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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
		{Method: "GET", Path: "/api/v1/content-blocks/{targetType}/{targetKey}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/sitemap.xml", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/{$}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
		{Method: "GET", Path: "/pages/{slug}", Surface: catalog.VisibilitySurfaceCatalog, EnforceIn: "PR-1057"},
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
		{Method: "POST", Path: "/checkout/confirm", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "PR-1058"},
		{Method: "GET", Path: "/api/v1/carts/{cartId}", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/cart", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/fragments/mini-cart", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/checkout/address", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/checkout/shipping", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "POST", Path: "/checkout/shipping", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/checkout/payment", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "POST", Path: "/checkout/payment", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
		{Method: "GET", Path: "/checkout/confirm", Surface: catalog.VisibilitySurfacePurchasable, EnforceIn: "out-of-scope"},
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

// productVisibilityWireExclusions are public routes that look product-adjacent
// in wire_routes.go but are intentionally not axis-gated (must stay listed).
var productVisibilityWireExclusions = map[string]string{
	"GET /api/v1/categories":           "category tree only — no product payloads",
	"GET /api/v1/categories/{id}":      "category entity only",
	"GET /categories":                  "HTML category index — no product listing",
	"GET /api/v1/pages/{slug}":         "CMS page JSON without carousel hydration (blocks are separate)",
	"POST /api/v1/carts":               "create empty cart — no product payload",
	"POST /cart/remove":                "line removal — no purchasability discovery",
	"POST /fragments/cart/remove":      "fragment line removal",
	"GET /fragments/cart-count":        "badge count only",
	"POST /api/v1/carts/{cartId}/coupon":   "coupon apply — no product discovery",
	"DELETE /api/v1/carts/{cartId}/coupon": "coupon remove",
	"DELETE /api/v1/carts/{cartId}/items/{variantId}": "line remove",
}

func TestPublicProductVisibilityRoutes_SyncWithWireRoutes(t *testing.T) {
	t.Parallel()

	registered := parseWireRoutePatterns(t)
	audited := make(map[string]struct{})
	for _, r := range storefront.PublicProductVisibilityRoutes() {
		audited[r.Method+" "+r.Path] = struct{}{}
	}

	for key, reason := range productVisibilityWireExclusions {
		if _, ok := registered[key]; !ok {
			t.Errorf("exclusion %q not found in wire_routes.go — remove stale exclusion (%s)", key, reason)
		}
		if _, ok := audited[key]; ok {
			t.Errorf("exclusion %q is also in PublicProductVisibilityRoutes — pick one", key)
		}
	}

	for key := range registered {
		method, path, ok := strings.Cut(key, " ")
		if !ok {
			t.Fatalf("bad registered key %q", key)
		}
		if !productVisibilityCandidate(method, path) {
			continue
		}
		if _, ok := audited[key]; ok {
			continue
		}
		if _, ok := productVisibilityWireExclusions[key]; ok {
			continue
		}
		t.Errorf("wire_routes.go registers %s but it is neither in PublicProductVisibilityRoutes nor productVisibilityWireExclusions", key)
	}

	for key := range audited {
		if _, ok := registered[key]; !ok {
			t.Errorf("PublicProductVisibilityRoutes has %s but wire_routes.go does not register it", key)
		}
	}
}

func productVisibilityCandidate(method, path string) bool {
	if strings.HasPrefix(path, "/api/v1/admin") {
		return false
	}
	switch {
	case strings.Contains(path, "/products"):
		return true
	case strings.Contains(path, "/search"):
		return true
	case path == "/sitemap.xml":
		return true
	case strings.Contains(path, "/cart") || strings.Contains(path, "/carts"):
		return true
	case strings.Contains(path, "/checkout"):
		return true
	case strings.Contains(path, "/content-blocks"):
		return true
	case path == "/{$}" || path == "/":
		return true
	case path == "/pages/{slug}":
		return true
	case path == "/categories/{slug}":
		return true
	case path == "/categories" || path == "/api/v1/categories" || path == "/api/v1/categories/{id}":
		return true // candidates; excluded above if not product listing
	case path == "/api/v1/pages/{slug}":
		return true
	case strings.HasPrefix(path, "/fragments/"):
		return strings.Contains(path, "cart") ||
			strings.Contains(path, "search") ||
			strings.Contains(path, "recently-viewed")
	default:
		return false
	}
}

func parseWireRoutePatterns(t *testing.T) map[string]struct{} {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(thisFile), "../../../../cmd/api/wire_routes.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read wire_routes.go: %v", err)
	}
	re := regexp.MustCompile(`router\.(?:HandleFunc|Handle)\("((?:GET|POST|PUT|DELETE) [^"]+)"`)
	matches := re.FindAllStringSubmatch(string(content), -1)
	if len(matches) < 50 {
		t.Fatalf("parsed %d routes from wire_routes.go, expected many more — regex broken?", len(matches))
	}
	out := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		out[m[1]] = struct{}{}
	}
	return out
}
