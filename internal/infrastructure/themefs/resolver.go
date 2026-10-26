package themefs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/akarso/shopanda/internal/domain/theme"
	"gopkg.in/yaml.v3"
)

// ResolveRootTheme resolves inherited template paths and metadata for loadDir.
func ResolveRootTheme(loadDir string) (theme.ResolvedTemplates, theme.Theme, error) {
	absLoadDir, err := filepath.Abs(loadDir)
	if err != nil {
		return theme.ResolvedTemplates{}, theme.Theme{}, fmt.Errorf("themefs: resolve path: %w", err)
	}

	meta, err := loadThemeYAML(filepath.Join(absLoadDir, "theme.yaml"))
	if err != nil {
		return theme.ResolvedTemplates{}, theme.Theme{}, fmt.Errorf("themefs: load metadata: %w", err)
	}

	boundary := theme.ThemeBoundary(absLoadDir)
	resolved, err := resolveThemeTemplates(absLoadDir, boundary, make(map[string]struct{}))
	if err != nil {
		return theme.ResolvedTemplates{}, theme.Theme{}, err
	}
	if len(resolved.PageFiles) == 0 {
		return theme.ResolvedTemplates{}, theme.Theme{}, fmt.Errorf("themefs: no page templates found for %q", absLoadDir)
	}
	return resolved, meta, nil
}

func resolveThemeTemplates(themeDir, boundary string, visited map[string]struct{}) (theme.ResolvedTemplates, error) {
	absDir, err := filepath.Abs(themeDir)
	if err != nil {
		return theme.ResolvedTemplates{}, fmt.Errorf("themefs: resolve path: %w", err)
	}
	if _, ok := visited[absDir]; ok {
		return theme.ResolvedTemplates{}, fmt.Errorf("themefs: circular parent chain at %q", absDir)
	}
	visited[absDir] = struct{}{}

	meta, err := loadThemeYAML(filepath.Join(absDir, "theme.yaml"))
	if err != nil {
		return theme.ResolvedTemplates{}, fmt.Errorf("themefs: load metadata: %w", err)
	}

	merged := theme.NewResolvedTemplates()
	if parent := strings.TrimSpace(meta.Parent); parent != "" {
		parentDir, err := theme.ResolveParentPath(absDir, parent, boundary)
		if err != nil {
			return theme.ResolvedTemplates{}, err
		}
		if _, err := os.Stat(filepath.Join(parentDir, "theme.yaml")); err != nil {
			return theme.ResolvedTemplates{}, fmt.Errorf("themefs: parent %q not found: %w", parent, err)
		}
		merged, err = resolveThemeTemplates(parentDir, boundary, visited)
		if err != nil {
			return theme.ResolvedTemplates{}, err
		}
	}

	templatePaths, err := globTemplatePaths(absDir)
	if err != nil {
		return theme.ResolvedTemplates{}, err
	}
	theme.OverlayTemplatePaths(&merged, templatePaths)
	if err := overlayLayoutFile(absDir, &merged); err != nil {
		return theme.ResolvedTemplates{}, err
	}
	return merged, nil
}

func globTemplatePaths(themeDir string) ([]string, error) {
	templatesDir := filepath.Join(themeDir, "templates")
	if _, err := os.Stat(templatesDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("themefs: stat templates: %w", err)
	}

	pattern := filepath.Join(templatesDir, "*.html")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("themefs: glob templates: %w", err)
	}
	return matches, nil
}

func loadThemeYAML(path string) (theme.Theme, error) {
	f, err := os.Open(path)
	if err != nil {
		return theme.Theme{}, err
	}
	defer f.Close()

	var t theme.Theme
	if err := yaml.NewDecoder(f).Decode(&t); err != nil {
		return theme.Theme{}, err
	}
	if t.Name == "" {
		return theme.Theme{}, fmt.Errorf("themefs: name is required in theme.yaml")
	}
	return t, nil
}
