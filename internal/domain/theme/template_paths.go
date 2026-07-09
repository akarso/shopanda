package theme

import (
	"path/filepath"
	"sort"
)

// TemplatePaths holds resolved inherited template file paths.
type TemplatePaths struct {
	Layout   string
	Partials map[string]string
	Pages    map[string]string
}

// ResolveTemplatePaths returns inherited template file paths for loadDir.
func ResolveTemplatePaths(loadDir string) (TemplatePaths, error) {
	resolved, _, err := resolveRootTheme(loadDir)
	if err != nil {
		return TemplatePaths{}, err
	}
	return templatePathsFromResolved(resolved), nil
}

func templatePathsFromResolved(resolved resolvedTemplates) TemplatePaths {
	return TemplatePaths{
		Layout:   resolved.layoutFile,
		Partials: resolved.partialFiles,
		Pages:    resolved.pageFiles,
	}
}

// OrderedPaths returns layout, partial, and page paths in stable order.
func (p TemplatePaths) OrderedPaths() []string {
	paths := make([]string, 0, 1+len(p.Partials)+len(p.Pages))
	if p.Layout != "" {
		paths = append(paths, p.Layout)
	}
	for _, path := range p.Partials {
		paths = append(paths, path)
	}
	for _, path := range p.Pages {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// Basename returns the template filename for an absolute path.
func Basename(path string) string {
	return filepath.Base(path)
}
