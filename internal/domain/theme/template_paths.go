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

// TemplatePathsFromResolved maps resolved templates to a path view.
func TemplatePathsFromResolved(resolved ResolvedTemplates) TemplatePaths {
	return TemplatePaths{
		Layout:   resolved.LayoutFile,
		Partials: resolved.PartialFiles,
		Pages:    resolved.PageFiles,
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
