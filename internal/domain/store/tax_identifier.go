package store

import (
	"errors"
	"strings"
)

// TaxIdentifier is one typed tax ID for a store (type is a registry code; see PR-1201).
type TaxIdentifier struct {
	StoreID string
	Type    string
	Value   string
}

// NewTaxIdentifier constructs a tax ID. type and value must be non-empty after trim.
// Type is lowercased for stable registry matching; value is trimmed only.
func NewTaxIdentifier(storeID, typ, value string) (TaxIdentifier, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: store id must not be empty")
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: type must not be empty")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: value must not be empty")
	}
	return TaxIdentifier{
		StoreID: storeID,
		Type:    typ,
		Value:   value,
	}, nil
}

// NewTaxIdentifierFromDB reconstructs TaxIdentifier from persistence (no re-validation).
func NewTaxIdentifierFromDB(storeID, typ, value string) TaxIdentifier {
	return TaxIdentifier{
		StoreID: storeID,
		Type:    typ,
		Value:   value,
	}
}
