package storefront_test

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	extensionapp "github.com/akarso/shopanda/internal/application/extension"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
)

func TestCheckoutConfirmTemplate_RendersImageAndEscapesText(t *testing.T) {
	assertThemeLineDisplayTemplate(t, "themes/default/templates/checkout_confirm.html", map[string]interface{}{
		"Confirmation": map[string]interface{}{
			"OrderID":     "ord-1",
			"TotalText":   "EUR 10.00",
			"Notice":      "Thanks",
			"ContinueURL": "/products",
			"Items":       sampleLineDisplayItems(),
		},
	})
}

func TestAccountOrderDetailTemplate_RendersImageAndEscapesText(t *testing.T) {
	assertThemeLineDisplayTemplate(t, "themes/default/templates/account_order_detail.html", map[string]interface{}{
		"OrderID":   "ord-1",
		"DateText":  "today",
		"Status":    "paid",
		"TotalText": "EUR 10.00",
		"BackURL":   "/account/orders",
		"ReturnsURL": "/account/orders/ord-1/returns",
		"AccountNav": map[string]string{},
		"Items":      sampleLineDisplayItems(),
	})
}

func sampleLineDisplayItems() []storefront.StorefrontAccountOrderItem {
	return []storefront.StorefrontAccountOrderItem{{
		Name:          "Widget",
		SKU:           "SKU-1",
		VariantID:     "var-1",
		Quantity:      1,
		LineTotalText: "EUR 10.00",
		Extensions: []storefront.StorefrontOrderLineExtensionDisplay{
			{FieldCode: "plugin.thumb", Label: "Design", Kind: extensionapp.DisplayKindImage, Value: "https://cdn.example/t.png?a=1&b=2", Alt: `alt "x"`},
			{FieldCode: "note", Label: "Note", Kind: extensionapp.DisplayKindText, Value: `say <b>hi</b>`},
		},
	}}
}

func assertThemeLineDisplayTemplate(t *testing.T, rel string, data interface{}) {
	t.Helper()
	root := findRepoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := `{{ define "_account_nav.html" }}{{ end }}` + "\n" + string(body) + "\n{{ template \"content\" . }}\n"
	tmpl, err := template.New(filepath.Base(rel)).Parse(src)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}
	var buf strings.Builder
	if err := tmpl.ExecuteTemplate(&buf, "content", data); err != nil {
		t.Fatalf("execute %s: %v", rel, err)
	}
	out := buf.String()
	if !strings.Contains(out, `src="https://cdn.example/t.png?a=1&amp;b=2"`) {
		t.Fatalf("%s missing escaped https img: %s", rel, out)
	}
	if strings.Contains(out, "http://") {
		t.Fatalf("%s must not invent http img: %s", rel, out)
	}
	if !strings.Contains(out, "say &lt;b&gt;hi&lt;/b&gt;") {
		t.Fatalf("%s text must be escaped: %s", rel, out)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
