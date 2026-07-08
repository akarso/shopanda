package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type resolvedTemplates struct {
	layoutFile string
	pageFiles  map[string]string // page name -> absolute file path
}

func resolveRootTheme(loadDir string) (resolvedTemplates, Theme, error) {
	absLoadDir, err := filepath.Abs(loadDir)
	if err != nil {
		return resolvedTemplates{}, Theme{}, fmt.Errorf("theme: resolve path: %w", err)
	}

	meta, err := loadThemeYAML(filepath.Join(absLoadDir, "theme.yaml"))
	if err != nil {
		return resolvedTemplates{}, Theme{}, fmt.Errorf("theme: load metadata: %w", err)
	}

	boundary := themeBoundary(absLoadDir)
	resolved, err := resolveThemeTemplates(absLoadDir, boundary, make(map[string]struct{}))
	if err != nil {
		return resolvedTemplates{}, Theme{}, err
	}
	if len(resolved.pageFiles) == 0 {
		return resolvedTemplates{}, Theme{}, fmt.Errorf("theme: no page templates found for %q", absLoadDir)
	}
	return resolved, meta, nil
}

func resolveThemeTemplates(themeDir, boundary string, visited map[string]struct{}) (resolvedTemplates, error) {
	absDir, err := filepath.Abs(themeDir)
	if err != nil {
		return resolvedTemplates{}, fmt.Errorf("theme: resolve path: %w", err)
	}
	if _, ok := visited[absDir]; ok {
		return resolvedTemplates{}, fmt.Errorf("theme: circular parent chain at %q", absDir)
	}
	visited[absDir] = struct{}{}

	meta, err := loadThemeYAML(filepath.Join(absDir, "theme.yaml"))
	if err != nil {
		return resolvedTemplates{}, fmt.Errorf("theme: load metadata: %w", err)
	}

	merged := resolvedTemplates{pageFiles: make(map[string]string)}
	if parent := strings.TrimSpace(meta.Parent); parent != "" {
		parentDir, err := resolveParentThemeDir(absDir, parent, boundary)
		if err != nil {
			return resolvedTemplates{}, err
		}
		merged, err = resolveThemeTemplates(parentDir, boundary, visited)
		if err != nil {
			return resolvedTemplates{}, err
		}
	}

	if err := overlayThemeTemplates(absDir, &merged); err != nil {
		return resolvedTemplates{}, err
	}
	return merged, nil
}

func themeBoundary(loadRoot string) string {
	return filepath.Clean(filepath.Join(loadRoot, ".."))
}

func resolveParentThemeDir(childDir, parentRef, boundary string) (string, error) {
	parentRef = strings.TrimSpace(parentRef)
	if parentRef == "" {
		return "", fmt.Errorf("theme: parent must not be empty")
	}
	if filepath.IsAbs(parentRef) {
		return "", fmt.Errorf("theme: parent %q must be a relative path", parentRef)
	}

	parentDir := filepath.Clean(filepath.Join(childDir, parentRef))
	if !isPathWithinBoundary(parentDir, boundary) {
		return "", fmt.Errorf("theme: parent %q resolves outside allowed theme boundary", parentRef)
	}
	if _, err := os.Stat(filepath.Join(parentDir, "theme.yaml")); err != nil {
		return "", fmt.Errorf("theme: parent %q not found: %w", parentRef, err)
	}
	return parentDir, nil
}

func isPathWithinBoundary(target, boundary string) bool {
	target = filepath.Clean(target)
	boundary = filepath.Clean(boundary)
	rel, err := filepath.Rel(boundary, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func overlayThemeTemplates(themeDir string, merged *resolvedTemplates) error {
	templatesDir := filepath.Join(themeDir, "templates")
	if _, err := os.Stat(templatesDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("theme: stat templates: %w", err)
	}

	pattern := filepath.Join(templatesDir, "*.html")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("theme: glob templates: %w", err)
	}
	for _, m := range matches {
		base := filepath.Base(m)
		if base == "layout.html" {
			merged.layoutFile = m
			continue
		}
		name := strings.TrimSuffix(base, filepath.Ext(base))
		merged.pageFiles[name] = m
	}
	return nil
}
