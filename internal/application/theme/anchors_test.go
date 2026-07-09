package theme_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	themeapp "github.com/akarso/shopanda/internal/application/theme"
)

func defaultThemeDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "..", "themes", "default")
	return dir
}

func themeTestdataDir(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "domain", "theme", "testdata", name)
}

func TestDeclaredAnchorsFromDir_DefaultTheme(t *testing.T) {
	declared, err := themeapp.DeclaredAnchorsFromDir(defaultThemeDir(t))
	if err != nil {
		t.Fatalf("DeclaredAnchorsFromDir: %v", err)
	}

	declaredSet := make(map[string]struct{}, len(declared))
	for _, name := range declared {
		declaredSet[name] = struct{}{}
	}
	for _, want := range slots.StandardAnchorNames() {
		if _, ok := declaredSet[want]; !ok {
			t.Fatalf("missing standard anchor %q; declared: %v", want, declared)
		}
	}
}

func TestDeclaredAnchorsFromDir_InheritsParentSlotMarkers(t *testing.T) {
	declared, err := themeapp.DeclaredAnchorsFromDir(themeTestdataDir(t, "child_partial_footer"))
	if err != nil {
		t.Fatalf("DeclaredAnchorsFromDir: %v", err)
	}

	declaredSet := make(map[string]struct{}, len(declared))
	for _, name := range declared {
		declaredSet[name] = struct{}{}
	}
	if _, ok := declaredSet["layout.head"]; !ok {
		t.Fatalf("expected inherited layout.head from parent; declared: %v", declared)
	}
}

func TestValidateDeclaredAnchors_DefaultTheme(t *testing.T) {
	declared, err := themeapp.DeclaredAnchorsFromDir(defaultThemeDir(t))
	if err != nil {
		t.Fatalf("DeclaredAnchorsFromDir: %v", err)
	}
	unknown, err := themeapp.ValidateDeclaredAnchors(declared, false)
	if err != nil {
		t.Fatalf("ValidateDeclaredAnchors: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("default theme should have no unknown anchors: %v", unknown)
	}
}

func TestValidateDeclaredAnchors_UnknownAnchor(t *testing.T) {
	declared := []string{"pdp.info", "custom.widget"}
	unknown, err := themeapp.ValidateDeclaredAnchors(declared, false)
	if err != nil {
		t.Fatalf("ValidateDeclaredAnchors: %v", err)
	}
	if len(unknown) != 1 || unknown[0] != "custom.widget" {
		t.Fatalf("unknown = %v", unknown)
	}
}

func TestValidateDeclaredAnchors_StrictFails(t *testing.T) {
	_, err := themeapp.ValidateDeclaredAnchors([]string{"custom.widget"}, true)
	if err == nil {
		t.Fatal("expected strict validation error")
	}
}

