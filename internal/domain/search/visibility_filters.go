package search

import (
	"fmt"
	"strings"
)

// Filter keys for storefront visibility (PR-1079). Engines AND these when
// present as bool true. Purchasable is exposed for callers that need it;
// storefront search/suggest use VisibleInSearch (or Catalog on PLP) only.
const (
	FilterVisibleInSearch  = "visible_in_search"
	FilterVisibleInCatalog = "visible_in_catalog"
	FilterPurchasable      = "purchasable"
	FilterType             = "type"
	FilterStatus           = "status"
)

// Visibility axis strings for ApplyVisibilityAxisFilter — kept in the search
// package so storefront can avoid importing catalog solely for filter wiring.
const (
	VisibilityFilterSearch  = "search"
	VisibilityFilterCatalog = "catalog"
)

// StatusActive is the catalog lifecycle value indexed/filtered as storefront-safe.
const StatusActive = "active"

// ApplyVisibilityAxisFilter sets the corresponding visibility filter on q so
// SearchEngine implementations can exclude non-visible products at query
// time (Total/Facets stay consistent). Unknown axes are ignored.
func ApplyVisibilityAxisFilter(q *SearchQuery, axis string) {
	if q == nil {
		return
	}
	if q.Filters == nil {
		q.Filters = map[string]interface{}{}
	}
	switch axis {
	case VisibilityFilterSearch:
		q.Filters[FilterVisibleInSearch] = true
	case VisibilityFilterCatalog:
		q.Filters[FilterVisibleInCatalog] = true
	}
}

// ApplySuggestVisibilityAxisFilter sets the same axis filter on a SuggestQuery
// that ApplyVisibilityAxisFilter sets on SearchQuery.
func ApplySuggestVisibilityAxisFilter(q *SuggestQuery, axis string) {
	if q == nil {
		return
	}
	if q.Filters == nil {
		q.Filters = map[string]interface{}{}
	}
	switch axis {
	case VisibilityFilterSearch:
		q.Filters[FilterVisibleInSearch] = true
	case VisibilityFilterCatalog:
		q.Filters[FilterVisibleInCatalog] = true
	}
}

// FilterBoolTrue reports whether a Filters map value means true.
// Accepts bool, common string/number encodings from JSON/query maps.
func FilterBoolTrue(v interface{}) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		switch strings.ToLower(strings.TrimSpace(b)) {
		case "true", "1", "yes":
			return true
		}
		return false
	case int:
		return b == 1
	case int64:
		return b == 1
	case float64:
		return b == 1
	default:
		return false
	}
}

// SanitizeFilterType returns a recognised product-type filter value, or
// ("", false) when the raw filter is missing/invalid (do not apply).
func SanitizeFilterType(v interface{}) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	switch s {
	case "simple", "virtual", "bundle", "grouped", "configurable", "downloadable":
		return s, true
	default:
		return "", false
	}
}

// MeiliVisibilityEqualsTrue builds a Meilisearch filter that matches documents
// where attr is true OR the attribute is absent. Absent fields must fail open
// until a full reindex writes the new attrs (Postgres columns DEFAULT true;
// Meili empty docs otherwise disappear under a strict `attr = true`).
func MeiliVisibilityEqualsTrue(attr string) string {
	return fmt.Sprintf("(%s = true OR %s NOT EXISTS)", attr, attr)
}

// MeiliStatusActiveOrMissing builds a Meilisearch filter that matches documents
// where status is active OR the attribute is absent. Pre-PR-1079 index docs
// may lack status; a strict `status = "active"` would hide every unreindexed
// hit (deploy outage). Keep status filterable; fail open until reindex.
func MeiliStatusActiveOrMissing() string {
	return fmt.Sprintf("(%s = %q OR %s NOT EXISTS)", FilterStatus, StatusActive, FilterStatus)
}
