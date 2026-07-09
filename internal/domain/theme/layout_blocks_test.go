package theme_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	themeapp "github.com/akarso/shopanda/internal/application/theme"
	"github.com/akarso/shopanda/internal/domain/theme"
)

func TestLayoutBlocks_ProductPageReorder(t *testing.T) {
	dir := t.TempDir()
	writeThemeWithLayout(t, dir, `
containers:
  pdp.info:
    blocks:
      - actions
      - meta
      - description
      - composition
      - detail_grid
`, `{{ define "title" }}P{{ end }}
{{ define "content" }}
{{slot_container "pdp.info"}}
<div class="body">
{{layout_blocks "pdp.info"}}
{{block "meta"}}<div class="meta">META</div>{{/block}}
{{block "description"}}<p class="desc">DESC</p>{{/block}}
{{block "composition"}}<p class="comp">COMP</p>{{/block}}
{{block "detail_grid"}}<dl class="grid">GRID</dl>{{/block}}
{{block "actions"}}<div class="actions">ACT</div>{{/block}}
{{/layout_blocks}}
</div>
{{/slot_container}}
{{ end }}
{{ template "layout.html" . }}`)

	engine, err := themeapp.Load(dir, theme.WithSlotSource(nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	if err := engine.Render(&buf, "product", map[string]interface{}{
		"Layout":  map[string]interface{}{"SiteName": "Shopanda"},
		"Product": map[string]interface{}{"Name": "Demo", "Slug": "demo", "Status": "active"},
		"Blocks":  []interface{}{},
	}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()
	actions := strings.Index(out, `class="actions"`)
	meta := strings.Index(out, `class="meta"`)
	desc := strings.Index(out, `class="desc"`)
	if actions < 0 || meta < 0 || desc < 0 {
		t.Fatalf("missing markers in output:\n%s", out)
	}
	if !(actions < meta && meta < desc) {
		t.Fatalf("expected actions before meta before description, got:\n%s", out)
	}
}

func writeThemeWithLayout(t *testing.T, dir, layoutYAML, productHTML string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "theme.yaml"), []byte("name: layout-test\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "layout.yaml"), []byte("version: \"1\"\n"+layoutYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	templates := filepath.Join(dir, "templates")
	if err := os.MkdirAll(templates, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templates, "product.html"), []byte(productHTML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templates, "layout.html"), []byte(`<html><body>{{ template "content" . }}</body></html>`), 0o644); err != nil {
		t.Fatal(err)
	}
}
