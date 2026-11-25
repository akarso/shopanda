package store

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// taxTypePattern matches registry codes (snake tokens: pl_nip, eu_vat).
var taxTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// TaxIdentifier is one typed tax ID for a store (type is a registry code; see PR-1201).
// Value is trimmed only — no digit folding; "123-456" and "123456" stay distinct.
type TaxIdentifier struct {
	StoreID string
	Type    string
	Value   string
}

// NewTaxIdentifier constructs a tax ID. Type must match ^[a-z][a-z0-9_]*$
// (after trim + lower) and both type and value are length-capped.
func NewTaxIdentifier(storeID, typ, value string) (TaxIdentifier, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: store id must not be empty")
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: type must not be empty")
	}
	if utf8.RuneCountInString(typ) > maxTaxTypeLen {
		return TaxIdentifier{}, fmt.Errorf("store tax identifier: type exceeds max length %d", maxTaxTypeLen)
	}
	if !taxTypePattern.MatchString(typ) {
		return TaxIdentifier{}, errors.New("store tax identifier: type must be a snake token (e.g. pl_nip)")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return TaxIdentifier{}, errors.New("store tax identifier: value must not be empty")
	}
	if utf8.RuneCountInString(value) > maxTaxValueLen {
		return TaxIdentifier{}, fmt.Errorf("store tax identifier: value exceeds max length %d", maxTaxValueLen)
	}
	return TaxIdentifier{
		StoreID: storeID,
		Type:    typ,
		Value:   value,
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
