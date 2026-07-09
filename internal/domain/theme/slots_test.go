package theme_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
	"github.com/akarso/shopanda/internal/domain/theme"
)

type registrySlotSource struct {
	reg *slots.Registry
}

func (s registrySlotSource) Render(anchor, placement string, data interface{}) string {
	p, err := slots.ParsePlacement(placement)
	if err != nil {
		return ""
	}
	return s.reg.Render(anchor, p, data)
}

func slotSource(reg *slots.Registry) theme.SlotSource {
	return registrySlotSource{reg: reg}
}

func TestSlotContainer_PlacementsInDOMOrder(t *testing.T) {
	dir := t.TempDir()
	writeThemeFiles(t, dir, `{{ define "title" }}Slots{{ end }}
{{ define "content" }}
{{slot_container "pdp.price"}}
<div class="product-price">{{.Price}}</div>
{{/slot_container}}
{{ end }}
{{ template "layout.html" . }}`, `<!DOCTYPE html><html><body>{{ template "content" . }}</body></html>`)

	reg := slots.NewRegistry(nil)
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementBefore, 100, "test", func(ctx *slots.RenderContext) string {
		return "<!--before-->"
	})
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementPrepend, 100, "test", func(ctx *slots.RenderContext) string {
		return "<!--prepend-->"
	})
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementAppend, 100, "test", func(ctx *slots.RenderContext) string {
		return "<!--append-->"
	})
	_ = reg.RegisterRenderer("pdp.price", slots.PlacementAfter, 100, "test", func(ctx *slots.RenderContext) string {
		return "<!--after-->"
	})

	engine, err := themeapp.Load(dir, theme.WithSlotSource(slotSource(reg)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := engine.Render(&buf, "product", struct{ Price string }{Price: "9.99"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	before := strings.Index(out, "<!--before-->")
	open := strings.Index(out, `<div class="product-price">`)
	prepend := strings.Index(out, "<!--prepend-->")
	price := strings.Index(out, "9.99")
	appendPos := strings.Index(out, "<!--append-->")
	closePos := strings.Index(out, "</div>")
	after := strings.Index(out, "<!--after-->")

	for name, pos := range map[string]int{
		"before": before, "open": open, "prepend": prepend, "price": price,
		"append": appendPos, "closePos": closePos, "after": after,
	} {
		if pos < 0 {
			t.Fatalf("missing %s in output:\n%s", name, out)
		}
	}
	if !(before < open && open < prepend && prepend < price && price < appendPos && appendPos < closePos && closePos < after) {
		t.Fatalf("slot placement order wrong:\n%s", out)
	}
}

func TestSlot_ExplicitMarkerUnknownAnchorNoop(t *testing.T) {
	dir := t.TempDir()
	writeThemeFiles(t, dir, `{{ define "content" }}{{slot . "missing.slot" "before"}}<p>ok</p>{{ end }}`, `<html><body>{{ template "content" . }}</body></html>`)

	engine, err := themeapp.Load(dir, theme.WithSlotSource(slotSource(slots.NewRegistry(nil))))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := engine.Render(&buf, "product", nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(buf.String(), "<p>ok</p>") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestSlot_TwoRenderersSamePlacementCompose(t *testing.T) {
	dir := t.TempDir()
	writeThemeFiles(t, dir, `{{ define "content" }}{{slot . "cart.summary" "append"}}<main>body</main>{{ end }}`, `<html><body>{{ template "content" . }}</body></html>`)

	reg := slots.NewRegistry(nil)
	_ = reg.RegisterRenderer("cart.summary", slots.PlacementAppend, 200, "b", func(ctx *slots.RenderContext) string { return "B" })
	_ = reg.RegisterRenderer("cart.summary", slots.PlacementAppend, 100, "a", func(ctx *slots.RenderContext) string { return "A" })

	engine, err := themeapp.Load(dir, theme.WithSlotSource(slotSource(reg)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := engine.Render(&buf, "product", nil); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "AB<main>body</main>") {
		t.Fatalf("output = %q, want composed append before main", got)
	}
}

func writeThemeFiles(t *testing.T, dir, productHTML, layoutHTML string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: slot-test\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	templates := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templates, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templates, "product.html"), []byte(productHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templates, "layout.html"), []byte(layoutHTML), 0o644); err != nil {
		t.Fatal(err)
	}
}
