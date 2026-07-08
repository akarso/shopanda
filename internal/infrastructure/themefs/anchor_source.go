package themefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/theme"
)

// AnchorSource loads theme template sources from the local filesystem.
type AnchorSource struct {
	ThemeDir string
}

// TemplateSources returns all HTML templates from <themeDir>/templates.
func (s AnchorSource) TemplateSources() ([]theme.TemplateSource, error) {
	templatesDir := filepath.Join(s.ThemeDir, "templates")
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil, fmt.Errorf("themefs: list templates: %w", err)
	}

	out := make([]theme.TemplateSource, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(templatesDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("themefs: read %s: %w", entry.Name(), err)
		}
		out = append(out, theme.TemplateSource{
			Name:    entry.Name(),
			Content: string(content),
		})
	}
	return out, nil
}
