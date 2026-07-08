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

func resolveThemeTemplates(themeDir string, visited map[string]struct{}) (resolvedTemplates, error) {
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
		parentDir, err := resolveParentThemeDir(absDir, parent)
		if err != nil {
			return resolvedTemplates{}, err
		}
		merged, err = resolveThemeTemplates(parentDir, visited)
		if err != nil {
			return resolvedTemplates{}, err
		}
	}

	if err := overlayThemeTemplates(absDir, &merged); err != nil {
		return resolvedTemplates{}, err
	}
	if len(merged.pageFiles) == 0 {
		return resolvedTemplates{}, fmt.Errorf("theme: no page templates found for %q", absDir)
	}
	return merged, nil
}

func resolveParentThemeDir(childDir, parentRef string) (string, error) {
	parentRef = strings.TrimSpace(parentRef)
	if parentRef == "" {
		return "", fmt.Errorf("theme: parent must not be empty")
	}
	var parentDir string
	if filepath.IsAbs(parentRef) {
		parentDir = filepath.Clean(parentRef)
	} else {
		parentDir = filepath.Clean(filepath.Join(childDir, parentRef))
	}
	if _, err := os.Stat(filepath.Join(parentDir, "theme.yaml")); err != nil {
		return "", fmt.Errorf("theme: parent %q not found: %w", parentRef, err)
	}
	return parentDir, nil
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
