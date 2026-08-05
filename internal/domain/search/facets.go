package search

// Reserved facet map keys used by search backends for category navigation.
// Attribute codes must not collide with these keys or category facets are overwritten.
var reservedFacetKeys = map[string]struct{}{
	"category":    {},
	"category_id": {},
}

// ReservedFacetKey reports whether code is reserved for engine-managed category facets.
func ReservedFacetKey(code string) bool {
	_, ok := reservedFacetKeys[code]
	return ok
}
