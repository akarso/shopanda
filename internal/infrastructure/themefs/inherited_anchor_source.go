package themefs

import (
	"fmt"
	"os"

	"github.com/akarso/shopanda/internal/domain/theme"
)

// InheritedAnchorSource loads inherited theme template sources from the filesystem.
type InheritedAnchorSource struct {
	ThemeDir string
}

// TemplateSources returns layout, partial, and page templates from the inherited theme chain.
func (s InheritedAnchorSource) TemplateSources() ([]theme.TemplateSource, error) {
	resolved, _, err := ResolveRootTheme(s.ThemeDir)
	if err != nil {
		return nil, fmt.Errorf("themefs: resolve inherited templates: %w", err)
	}

	paths := theme.TemplatePathsFromResolved(resolved).OrderedPaths()
	out := make([]theme.TemplateSource, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("themefs: read %s: %w", theme.Basename(path), err)
		}
		out = append(out, theme.TemplateSource{
			Name:    theme.Basename(path),
			Content: string(content),
		})
	}
	return out, nil
}
