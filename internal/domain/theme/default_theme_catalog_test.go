package theme_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/infrastructure/themefs"
)

func TestDefaultTheme_DeclaresStandardAnchors(t *testing.T) {
	declared, err := theme.DeclaredAnchors(themefs.AnchorSource{ThemeDir: defaultThemeDir(t)})
	if err != nil {
		t.Fatalf("DeclaredAnchors: %v", err)
	}

	declaredSet := make(map[string]struct{}, len(declared))
	for _, name := range declared {
		declaredSet[name] = struct{}{}
	}

	for _, want := range slots.StandardAnchorNames() {
		if _, ok := declaredSet[want]; !ok {
			t.Fatalf("default theme missing standard anchor %q; declared: %v", want, declared)
		}
	}
}
