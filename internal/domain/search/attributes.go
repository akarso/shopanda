package search

import "strings"

// AttributeFilterPrefix is the SearchQuery.Filters key prefix for product attribute filters.
const AttributeFilterPrefix = "attr_"

// AttributeFilterKey returns the filter map key for a catalog attribute code.
func AttributeFilterKey(code string) string {
	return AttributeFilterPrefix + code
}

// AttributeCodeFromFilterKey parses an attr_<code> filter key.
func AttributeCodeFromFilterKey(key string) (string, bool) {
	if !strings.HasPrefix(key, AttributeFilterPrefix) {
		return "", false
	}
	code := strings.TrimPrefix(key, AttributeFilterPrefix)
	if !AttributeCodeValid(code) {
		return "", false
	}
	return code, true
}

// AttributeCodeValid reports whether code is safe for attribute filter/facet fields.
func AttributeCodeValid(code string) bool {
	if code == "" || len(code) > 64 {
		return false
	}
	for i, r := range code {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' && i > 0 {
			continue
		}
		if r == '_' && i > 0 {
			continue
		}
		return false
	}
	return true
}

// AttributeIndexField returns the Meilisearch document field for an attribute code.
func AttributeIndexField(code string) string {
	return AttributeFilterKey(code)
}
