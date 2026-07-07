package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	slotContainerAnchorPattern = regexp.MustCompile(`\{\{\s*slot_container\s+"([^"]+)"`)
	slotExplicitAnchorPattern  = regexp.MustCompile(`\{\{\s*slot\s+\.\s+"([^"]+)"`)
)

// DeclaredAnchors scans theme template files for slot_container and slot markers.
func DeclaredAnchors(themeDir string) ([]string, error) {
	templatesDir := filepath.Join(themeDir, "templates")
	entries, err := os.ReadDir(templatesDir)
	if err != nil {
		return nil, fmt.Errorf("theme: list templates: %w", err)
	}

	seen := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		source, err := os.ReadFile(filepath.Join(templatesDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("theme: read %s: %w", entry.Name(), err)
		}
		for _, name := range extractDeclaredAnchors(string(source)) {
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
