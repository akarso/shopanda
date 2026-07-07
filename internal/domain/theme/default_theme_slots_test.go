package theme_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
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
	for _, anchor := range []string{
		"layout.head",
		"layout.body_start",
		"layout.header",
		"layout.nav",
		"layout.main",
		"layout.footer",
		"layout.body_end",
	} {
		anchor := anchor
		_ = reg.RegisterRenderer(anchor, slots.PlacementAppend, 100, "test", func(ctx *slots.RenderContext) string {
			return "<!--" + anchor + "-->"
		})
	}

	engine, err := theme.Load(defaultThemeDir(t), theme.WithSlotSource(slotSource(reg)))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

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

	out := buf.String()
	for _, anchor := range []string{
		"layout.head",
		"layout.body_start",
		"layout.header",
		"layout.nav",
		"layout.main",
		"layout.footer",
		"layout.body_end",
	} {
		marker := "<!--" + anchor + "-->"
		if !strings.Contains(out, marker) {
			t.Fatalf("missing slot output %s in:\n%s", marker, out)
		}
	}
}
