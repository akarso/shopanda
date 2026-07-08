package theme_test

import (
	"testing"

	"github.com/akarso/shopanda/internal/application/slots"
	"github.com/akarso/shopanda/internal/domain/theme"
)

func TestDeclaredAnchorsFromDir_DefaultTheme(t *testing.T) {
	declared, err := theme.DeclaredAnchorsFromDir(defaultThemeDir(t))
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

func TestDeclaredAnchorsFromDir_InheritsParentTemplates(t *testing.T) {
	declared, err := theme.DeclaredAnchorsFromDir("testdata/child_partial_footer")
	if err != nil {
		t.Fatalf("DeclaredAnchorsFromDir: %v", err)
	}
	if len(declared) != 0 {
		t.Fatalf("child_partial_footer has no slot markers; got %v", declared)
	}
}
