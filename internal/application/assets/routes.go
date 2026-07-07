package assets

import "strings"

// routeMatches reports whether path matches pattern.
// Empty patterns never match; omit Routes on a manifest to match all pages.
func routeMatches(pattern, path string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return path == pattern
}

func manifestMatchesRoute(routes []string, path string) bool {
	if len(routes) == 0 {
		return true
	}
	for _, pattern := range routes {
		if routeMatches(pattern, path) {
			return true
		}
	}
	return false
}
