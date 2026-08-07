package theme_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
	"github.com/akarso/shopanda/internal/domain/theme"
)

func defaultThemeDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "themes", "default")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("default theme dir: %v", err)
	}
	return dir
}

func TestDefaultTheme_StandardLayoutSlotsRender(t *testing.T) {
	reg := slots.NewRegistry(nil)
	for _, anchor := range slots.StandardAnchorNames() {
		anchor := anchor
		_ = reg.RegisterRenderer(anchor, slots.PlacementAppend, 100, "test", func(ctx *slots.RenderContext) string {
			return "<!--" + anchor + "-->"
		})
	}

	engine, err := themeapp.Load(defaultThemeDir(t), theme.WithSlotSource(slotSource(reg)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	t.Run("home layout anchors", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "home", map[string]interface{}{
			"Layout": map[string]interface{}{
				"SiteName":   "Shopanda",
				"Nav":        []interface{}{},
				"Categories": nil,
				"Assets":     map[string]interface{}{},
			},
		}); err != nil {
			t.Fatalf("Render: %v", err)
		}
		assertSlotMarkers(t, buf.String(), []string{
			"layout.head", "layout.body_start", "layout.header", "layout.nav",
			"layout.main", "layout.footer", "layout.body_end", "home.hero",
		})
	})

	t.Run("product page anchors", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "product", map[string]interface{}{
			"Layout":  map[string]interface{}{"SiteName": "Shopanda", "Nav": []interface{}{}, "Assets": map[string]interface{}{}},
			"Product": map[string]interface{}{"Name": "Demo", "Slug": "demo", "Status": "active"},
			"Blocks":  []interface{}{},
		}); err != nil {
			t.Fatalf("Render: %v", err)
		}
		assertSlotMarkers(t, buf.String(), []string{"pdp.gallery", "pdp.info", "pdp.actions"})
	})

	t.Run("plp page anchors", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "product_list", map[string]interface{}{
			"Layout":        map[string]interface{}{"SiteName": "Shopanda", "Nav": []interface{}{}, "Assets": map[string]interface{}{}},
			"Title":         "Products",
			"Eyebrow":       "Catalog",
			"ResultSummary": "1 result",
			"View":          "grid",
			"GridURL":       "/products?view=grid",
			"ListURL":       "/products?view=list",
			"SortOptions":   []interface{}{},
		}); err != nil {
			t.Fatalf("Render: %v", err)
		}
		assertSlotMarkers(t, buf.String(), []string{"plp.toolbar"})
	})

	t.Run("cart page anchors", func(t *testing.T) {
		var buf bytes.Buffer
		if err := engine.Render(&buf, "cart", map[string]interface{}{
			"Layout": map[string]interface{}{"SiteName": "Shopanda", "Nav": []interface{}{}, "Assets": map[string]interface{}{}},
			"Items": []interface{}{
				map[string]interface{}{
					"ProductName": "Demo", "VariantID": "v1", "VariantSKU": "SKU",
					"Quantity": 1, "UnitPriceText": "$1", "LineTotalText": "$1",
				},
			},
			"Summary": map[string]interface{}{"ItemCount": 1, "TotalQuantity": 1, "SubtotalText": "$1"},
		}); err != nil {
			t.Fatalf("Render: %v", err)
		}
		assertSlotMarkers(t, buf.String(), []string{"cart.items", "cart.summary"})
	})

	t.Run("checkout page anchors", func(t *testing.T) {
		checkoutAnchors := []string{"checkout.progress", "checkout.panel", "checkout.summary"}
		layout := map[string]interface{}{"SiteName": "Shopanda", "Nav": []interface{}{}, "Assets": map[string]interface{}{}}
		address := map[string]interface{}{}
		pages := []struct {
			name string
			data map[string]interface{}
		}{
			{
				name: "checkout_address",
				data: map[string]interface{}{
					"Layout": layout, "CSRFToken": "tok",
					"Progress": []interface{}{map[string]interface{}{"Label": "Address", "Current": true}},
					"Items":    []interface{}{}, "Summary": map[string]interface{}{"TotalQuantity": 0, "SubtotalText": "$0"},
					"Countries": []interface{}{}, "Address": address,
				},
			},
			{
				name: "checkout_shipping",
				data: map[string]interface{}{
					"Layout": layout, "CSRFToken": "tok", "ContactEmail": "buyer@example.com",
					"Progress": []interface{}{map[string]interface{}{"Label": "Shipping", "Current": true}},
					"Address":  address,
					"Rates": []interface{}{
						map[string]interface{}{"Method": "flat", "Label": "Standard", "CostText": "$5", "Selected": true},
					},
					"Summary": map[string]interface{}{"SubtotalText": "$0"},
				},
			},
		}
		for _, page := range pages {
			page := page
			t.Run(page.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.Render(&buf, page.name, page.data); err != nil {
					t.Fatalf("Render: %v", err)
				}
				assertSlotMarkers(t, buf.String(), checkoutAnchors)
			})
		}
	})

	t.Run("account page anchors", func(t *testing.T) {
		layout := map[string]interface{}{"SiteName": "Shopanda", "Nav": []interface{}{}, "Assets": map[string]interface{}{}}
		accountNav := func(current string) map[string]interface{} {
			return map[string]interface{}{
				"OrdersURL": "/account/orders", "ReturnsURL": "/account/returns",
				"ProfileURL": "/account/profile", "AddressesURL": "/account/addresses",
				"PreferencesURL": "/account/preferences", "SecurityURL": "/account/security",
				"Current": current,
			}
		}
		pages := []struct {
			name string
			data map[string]interface{}
		}{
			{
				name: "account_orders",
				data: map[string]interface{}{
					"Layout": layout, "AccountNav": accountNav("orders"),
					"Orders": []interface{}{}, "EmptyMessage": "No orders",
				},
			},
			{
				name: "account_profile",
				data: map[string]interface{}{
					"Layout": layout, "AccountNav": accountNav("profile"),
					"CSRFToken": "tok", "Email": "buyer@example.com",
					"FirstName": "Ada", "LastName": "Lovelace",
				},
			},
		}
		for _, page := range pages {
			page := page
			t.Run(page.name, func(t *testing.T) {
				var buf bytes.Buffer
				if err := engine.Render(&buf, page.name, page.data); err != nil {
					t.Fatalf("Render: %v", err)
				}
				assertSlotMarkers(t, buf.String(), []string{"account.nav"})
			})
		}
	})
}

func assertSlotMarkers(t *testing.T, html string, anchors []string) {
	t.Helper()
	for _, anchor := range anchors {
		marker := "<!--" + anchor + "-->"
		if !strings.Contains(html, marker) {
			t.Fatalf("missing slot output %s", marker)
		}
	}
}
