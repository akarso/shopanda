package catalog

// TaxClassCode returns the tax class string used for rate-table lookup for a
// product type.
//
//	downloadable → "digital"
//	virtual      → "standard" (ESS / electronically supplied virtual needs a
//	               future opt-in; do not force digital without merchant choice)
//	all others   → "standard" (including bundle / grouped / configurable parents)
//
// Uses string literals rather than importing domain/tax so catalog stays free
// of a tax dependency (and avoids import cycles with application wiring).
func TaxClassCode(t Type) string {
	switch t {
	case TypeDownloadable:
		return "digital"
	default:
		return "standard"
	}
}
