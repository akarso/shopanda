package theme

import (
	"fmt"
	"strings"

	"github.com/akarso/shopanda/internal/application/slots"
	domtheme "github.com/akarso/shopanda/internal/domain/theme"
	"github.com/akarso/shopanda/internal/infrastructure/themefs"
)

// DeclaredAnchorsFromDir resolves inherited templates for loadDir and returns
// declared slot anchors from layout, partials, and page templates.
func DeclaredAnchorsFromDir(loadDir string) ([]string, error) {
	declared, err := domtheme.DeclaredAnchors(themefs.InheritedAnchorSource{ThemeDir: loadDir})
	if err != nil {
		return nil, fmt.Errorf("theme: declared anchors: %w", err)
	}
	return declared, nil
}

// ValidateDeclaredAnchors reports theme anchors outside the standard catalog.
// When strict is true, returns an error if any unknown anchors exist.
func ValidateDeclaredAnchors(declared []string, strict bool) ([]string, error) {
	unknown := slots.UnknownDeclaredAnchors(declared)
	if strict && len(unknown) > 0 {
		return unknown, fmt.Errorf("theme: unknown slot anchors: %s", strings.Join(unknown, ", "))
	}
	return unknown, nil
}
