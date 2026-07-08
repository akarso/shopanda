package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DeclaredAnchorsFromDir resolves inherited templates for loadDir and returns
// declared slot anchors from layout, partials, and page templates.
func DeclaredAnchorsFromDir(loadDir string) ([]string, error) {
	resolved, _, err := resolveRootTheme(loadDir)
	if err != nil {
		return nil, fmt.Errorf("theme: declared anchors: %w", err)
	}

	sources, err := templateSourcesFromResolved(resolved)
	if err != nil {
		return nil, err
	}
	return DeclaredAnchors(sliceTemplateSourceProvider{sources: sources})
}

type sliceTemplateSourceProvider struct {
	sources []TemplateSource
}

func (p sliceTemplateSourceProvider) TemplateSources() ([]TemplateSource, error) {
	return p.sources, nil
}

func templateSourcesFromResolved(resolved resolvedTemplates) ([]TemplateSource, error) {
	paths := make([]string, 0, 1+len(resolved.partialFiles)+len(resolved.pageFiles))
	if resolved.layoutFile != "" {
		paths = append(paths, resolved.layoutFile)
	}
	for _, path := range resolved.partialFiles {
		paths = append(paths, path)
	}
	for _, path := range resolved.pageFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	out := make([]TemplateSource, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("theme: read template %s: %w", filepath.Base(path), err)
		}
		out = append(out, TemplateSource{
			Name:    filepath.Base(path),
			Content: string(content),
		})
	}
	return out, nil
}
