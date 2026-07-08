package theme

import (
	"regexp"
	"sort"
	"strings"
)

var (
	slotContainerAnchorPattern = regexp.MustCompile(`\{\{\s*slot_container\s+"([^"]+)"`)
	slotExplicitAnchorPattern  = regexp.MustCompile(`\{\{\s*slot\s+\.\s+"([^"]+)"`)
)

// TemplateSource is a single template input for anchor extraction.
type TemplateSource struct {
	Name    string
	Content string
}

// TemplateSourceProvider provides template sources from any storage backend.
type TemplateSourceProvider interface {
	TemplateSources() ([]TemplateSource, error)
}

// DeclaredAnchors extracts declared slot anchors from provider templates.
func DeclaredAnchors(provider TemplateSourceProvider) ([]string, error) {
	sources, err := provider.TemplateSources()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	for _, source := range sources {
		for _, name := range extractDeclaredAnchors(source.Content) {
			seen[name] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func extractDeclaredAnchors(source string) []string {
	seen := make(map[string]struct{})
	for _, pattern := range []*regexp.Regexp{slotContainerAnchorPattern, slotExplicitAnchorPattern} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			if len(match) < 2 {
				continue
			}
			name := strings.TrimSpace(match[1])
			if name != "" {
				seen[name] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
