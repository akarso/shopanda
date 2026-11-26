package store

import "strings"

// imprintPublicTaxTypes are VAT-style identifiers safe for public imprint /
// storefront GET. Company registration, EIN, Steuernummer, SIRET, KVK, and
// generic stay admin-only until a consumer opts in.
var imprintPublicTaxTypes = map[string]struct{}{
	"eu_vat":         {},
	"pl_nip":         {},
	"de_ustidnr":     {},
	"fr_tva":         {},
	"it_partita_iva": {},
	"es_nif":         {},
	"nl_btw":         {},
	"uk_vat":         {},
}

// ImprintPublicTaxType reports whether a tax identifier type may appear on the
// public store identity payload.
func ImprintPublicTaxType(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	_, ok := imprintPublicTaxTypes[code]
	return ok
}

// InvoiceSellerTaxType reports whether a tax identifier may appear on the
// invoice PDF seller block. Same VAT-style allowlist as imprint — company
// registration, EIN, Steuernummer, SIRET, KVK, CRN, and generic stay off the
// customer PDF until explicitly opted in.
func InvoiceSellerTaxType(code string) bool {
	return ImprintPublicTaxType(code)
}
