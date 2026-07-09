package theme

import (
	"fmt"

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
