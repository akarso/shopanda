package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// taxTypePattern matches registry codes (snake tokens: pl_nip, eu_vat).
var taxTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// TaxIdentifier is one typed tax ID for a store (type is a registry code; see PR-1201).
// Value is a single-line field (same control rules as address lines); punctuation
// is kept — "123-456" and "123456" stay distinct.
type TaxIdentifier struct {
	StoreID string `json:"store_id"`
	Type    string `json:"type"`
	Value   string `json:"value"`
}

// NewTaxIdentifier constructs a tax ID. Type must match ^[a-z][a-z0-9_]*$
// (after trim + lower) and both type and value are length-capped.
func NewTaxIdentifier(storeID, typ, value string) (TaxIdentifier, error) {
	sid, err := normalizeStoreID(storeID)
	if err != nil {
		return TaxIdentifier{}, fmt.Errorf("store tax identifier: %w", err)
	}
	typNorm, err := normalizeSingleLine(typ, MaxTaxTypeLen, "type")
	if err != nil {
		return TaxIdentifier{}, fmt.Errorf("store tax identifier: %w", err)
	}
	typNorm = strings.ToLower(typNorm)
	if typNorm == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: type must not be empty")
	}
	if !taxTypePattern.MatchString(typNorm) {
		return TaxIdentifier{}, errors.New("store tax identifier: type must be a snake token (e.g. pl_nip)")
	}
	val, err := normalizeSingleLine(value, MaxTaxValueLen, "value")
	if err != nil {
		return TaxIdentifier{}, fmt.Errorf("store tax identifier: %w", err)
	}
	if val == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: value must not be empty")
	}
	return TaxIdentifier{
		StoreID: sid,
		Type:    typNorm,
		Value:   val,
	}, nil
}

// NewTaxIdentifierFromDB reconstructs TaxIdentifier from persistence (no re-validation).
func NewTaxIdentifierFromDB(storeID, typ, value string) *TaxIdentifier {
	return &TaxIdentifier{
		StoreID: storeID,
		Type:    typ,
		Value:   value,
	}
}
