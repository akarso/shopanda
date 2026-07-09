package slotsdemo_test

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
	"github.com/akarso/shopanda/plugins/slotsdemo"
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

func defaultThemeDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "themes", "default")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("default theme dir: %v", err)
	}
	return dir
}

func TestSlotsDemo_ProductPageDOMPositions(t *testing.T) {
	app := testApp(t, true)
	if err := slotsdemo.New().Init(app); err != nil {
		t.Fatalf("Init(): %v", err)
	}

	engine, err := themeapp.Load(defaultThemeDir(t), theme.WithSlotSource(registrySlotSource{reg: app.SlotRegistry()}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := engine.Render(&buf, "product", map[string]interface{}{
		"Layout":  map[string]interface{}{"SiteName": "Shopanda", "Nav": []interface{}{}, "Assets": map[string]interface{}{}},
		"Product": map[string]interface{}{"Name": "Demo", "Slug": "demo", "Status": "active"},
		"Blocks":  []interface{}{},
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := buf.String()

	if !strings.Contains(html, `data-slotsdemo="pdp-info"`) {
		t.Fatalf("missing PDP info slot output:\n%s", html)
	}
	if !strings.Contains(html, `data-slotsdemo="layout-footer"`) {
		t.Fatalf("missing layout footer slot output:\n%s", html)
	}

	assertBefore(t, html, `class="product-detail-card__actions"`, `data-slotsdemo="pdp-info"`)
	assertBefore(t, html, `data-slotsdemo="layout-footer"`, "</footer>")
	assertBefore(t, html, `data-slotsdemo="pdp-info"`, `data-slotsdemo="layout-footer"`)
}

func assertBefore(t *testing.T, html, before, after string) {
	t.Helper()
	beforeIdx := strings.Index(html, before)
	afterIdx := strings.Index(html, after)
	if beforeIdx < 0 {
		t.Fatalf("substring %q not found", before)
	}
	if afterIdx < 0 {
		t.Fatalf("substring %q not found", after)
	}
	if beforeIdx >= afterIdx {
		t.Fatalf("%q should appear before %q", before, after)
	}
}
