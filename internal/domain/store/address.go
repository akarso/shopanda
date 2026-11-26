package store

import (
	"fmt"
	"strings"
)

// AddressRole is one of the Magento-like store address slots.
type AddressRole string

const (
	AddressRoleGeneral        AddressRole = "general"
	AddressRoleInvoice        AddressRole = "invoice"
	AddressRoleShippingOrigin AddressRole = "shipping_origin"
	AddressRoleReturns        AddressRole = "returns"
)

// addressRoles is the stable ordered set for ParseAddressRole / admin index /
// public GET. Keep unexported so callers cannot mutate process-wide order.
var addressRoles = []AddressRole{
	AddressRoleGeneral,
	AddressRoleInvoice,
	AddressRoleShippingOrigin,
	AddressRoleReturns,
}

// AllAddressRoles returns every valid role in stable order (copy).
func AllAddressRoles() []AddressRole {
	out := make([]AddressRole, len(addressRoles))
	copy(out, addressRoles)
	return out
}

// ParseAddressRole normalizes and validates a role string against addressRoles.
func ParseAddressRole(v string) (AddressRole, error) {
	trimmed := strings.TrimSpace(v)
	r := AddressRole(strings.ToLower(trimmed))
	for _, allowed := range addressRoles {
		if r == allowed {
			return r, nil
		}
	}
	return "", fmt.Errorf("store address: invalid role %q", roleErrorLabel(trimmed))
}

// roleErrorLabel caps length and replaces disallowed controls/format chars with
// '?' so error text cannot smuggle newlines or header-injection bytes into logs.
func roleErrorLabel(v string) string {
	var b strings.Builder
	n := 0
	for _, r := range v {
		if n >= MaxAddressRoleLen {
			break
		}
		if isDisallowedLineControl(r) || isDisallowedFormatChar(r) {
			b.WriteByte('?')
		} else {
			b.WriteRune(r)
		}
		n++
	}
	return b.String()
}

// Address is a structured store address for one role.
type Address struct {
	StoreID  string      `json:"store_id"`
	Role     AddressRole `json:"role"`
	Line1    string      `json:"line1"`
	Line2    string      `json:"line2"`
	City     string      `json:"city"`
	Region   string      `json:"region"`
	Postcode string      `json:"postcode"`
	Country  string      `json:"country"` // ISO 3166-1 alpha-2 (ASCII A–Z)
	Phone    string      `json:"phone"`
}

// IsEmpty reports whether the address has no postal content for role fallback.
// Postal emptiness is line1, line2, city, region, and postcode — phone and
// country alone do not count, so a phone-only invoice row still falls back to
// general. A nil receiver is empty (safe for missing-row lookups).
// PR-1202 backfill should set general.line1 from store.address and
// general.country from store.Country.
func (a *Address) IsEmpty() bool {
	if a == nil {
		return true
	}
	return strings.TrimSpace(a.Line1) == "" &&
		strings.TrimSpace(a.Line2) == "" &&
		strings.TrimSpace(a.City) == "" &&
		strings.TrimSpace(a.Region) == "" &&
		strings.TrimSpace(a.Postcode) == ""
}

// NewAddress constructs a role address. Country must be a 2-letter ASCII ISO
// code (always required, including for placeholders). Other fields may be
// blank; country-only and phone-only rows are IsEmpty and participate in
// role fallback.
func NewAddress(
	storeID string,
	role AddressRole,
	line1, line2, city, region, postcode, country, phone string,
) (Address, error) {
	sid, err := normalizeStoreID(storeID)
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	role, err = ParseAddressRole(string(role))
	if err != nil {
		return Address{}, err
	}
	cty, err := NormalizeCountry(country)
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	l1, err := normalizeSingleLine(line1, MaxAddressLineLen, "line1")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	l2, err := normalizeSingleLine(line2, MaxAddressLineLen, "line2")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	cityNorm, err := normalizeSingleLine(city, MaxCityLen, "city")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	regionNorm, err := normalizeSingleLine(region, MaxRegionLen, "region")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	postNorm, err := normalizeSingleLine(postcode, MaxPostcodeLen, "postcode")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	phoneNorm, err := normalizeSingleLine(phone, MaxPhoneLen, "phone")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	return Address{
		StoreID:  sid,
		Role:     role,
		Line1:    l1,
		Line2:    l2,
		City:     cityNorm,
		Region:   regionNorm,
		Postcode: postNorm,
		Country:  cty,
		Phone:    phoneNorm,
	}, nil
}

// NewAddressFromDB reconstructs Address from persistence (no re-validation).
func NewAddressFromDB(
	storeID string,
	role AddressRole,
	line1, line2, city, region, postcode, country, phone string,
) *Address {
	return &Address{
		StoreID:  storeID,
		Role:     role,
		Line1:    line1,
		Line2:    line2,
		City:     city,
		Region:   region,
		Postcode: postcode,
		Country:  country,
		Phone:    phone,
	}
}
