package theme

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolvedTemplates holds inherited template file paths after overlay resolution.
type ResolvedTemplates struct {
	LayoutFile   string
	PartialFiles map[string]string
	PageFiles    map[string]string
	Layout       LayoutConfig
}

// NewResolvedTemplates creates an empty resolved template set.
func NewResolvedTemplates() ResolvedTemplates {
	return ResolvedTemplates{
		PartialFiles: make(map[string]string),
		PageFiles:    make(map[string]string),
	}
}

// ThemeBoundary returns the allowed parent lookup boundary for a loaded theme root.
func ThemeBoundary(loadRoot string) string {
	return filepath.Clean(filepath.Join(loadRoot, ".."))
}

// ResolveParentPath resolves a relative parent reference without filesystem checks.
func ResolveParentPath(childDir, parentRef, boundary string) (string, error) {
	parentRef = strings.TrimSpace(parentRef)
	if parentRef == "" {
		return "", fmt.Errorf("theme: parent must not be empty")
	}
	if filepath.IsAbs(parentRef) {
		return "", fmt.Errorf("theme: parent %q must be a relative path", parentRef)
	}

	parentDir := filepath.Clean(filepath.Join(childDir, parentRef))
	if !IsPathWithinBoundary(parentDir, boundary) {
		return "", fmt.Errorf("theme: parent %q resolves outside allowed theme boundary", parentRef)
	}
	return parentDir, nil
}

// IsPathWithinBoundary reports whether target stays within boundary.
func IsPathWithinBoundary(target, boundary string) bool {
	target = filepath.Clean(target)
	boundary = filepath.Clean(boundary)
	rel, err := filepath.Rel(boundary, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// OverlayTemplatePaths overlays template file paths onto merged (child wins by filename).
func OverlayTemplatePaths(merged *ResolvedTemplates, templatePaths []string) {
	if merged == nil {
		return
	}
	if merged.PartialFiles == nil {
		merged.PartialFiles = make(map[string]string)
	}
	if merged.PageFiles == nil {
		merged.PageFiles = make(map[string]string)
	}
	for _, path := range templatePaths {
		base := filepath.Base(path)
		switch {
		case base == "layout.html":
			merged.LayoutFile = path
		case IsPartialTemplate(base):
			name := strings.TrimSuffix(base, filepath.Ext(base))
			merged.PartialFiles[name] = path
		default:
			name := strings.TrimSuffix(base, filepath.Ext(base))
			merged.PageFiles[name] = path
		}
	}
}

// IsPartialTemplate reports whether a template filename is a layout partial.
func IsPartialTemplate(base string) bool {
	return strings.HasPrefix(base, "_") && strings.HasSuffix(base, ".html")
}
