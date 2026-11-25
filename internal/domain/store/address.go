package store

import (
	"errors"
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
	r := AddressRole(strings.TrimSpace(strings.ToLower(v)))
	for _, allowed := range addressRoles {
		if r == allowed {
			return r, nil
		}
	}
	return "", fmt.Errorf("store address: invalid role %q", strings.TrimSpace(v))
}

// Address is a structured store address for one role.
type Address struct {
	StoreID  string
	Role     AddressRole
	Line1    string
	Line2    string
	City     string
	Region   string
	Postcode string
	Country  string // ISO 3166-1 alpha-2 (ASCII A–Z)
	Phone    string
}

// IsEmpty reports whether the address has no content fields worth showing.
// Country alone does not count: a country-only placeholder and a missing row
// both fall through invoice → general and returns → shipping_origin → general.
// PR-1202 backfill should set general.line1 from store.address and
// general.country from store.Country.
func (a Address) IsEmpty() bool {
	return strings.TrimSpace(a.Line1) == "" &&
		strings.TrimSpace(a.Line2) == "" &&
		strings.TrimSpace(a.City) == "" &&
		strings.TrimSpace(a.Region) == "" &&
		strings.TrimSpace(a.Postcode) == "" &&
		strings.TrimSpace(a.Phone) == ""
}

// NewAddress constructs a role address. Country must be a 2-letter ASCII ISO
// code (always required, including for placeholders). Other fields may be
// blank; country-only rows are IsEmpty and participate in role fallback.
func NewAddress(
	storeID string,
	role AddressRole,
	line1, line2, city, region, postcode, country, phone string,
) (Address, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return Address{}, errors.New("store address: store id must not be empty")
	}
	role, err := ParseAddressRole(string(role))
	if err != nil {
		return Address{}, err
	}
	cty, err := NormalizeCountry(country)
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	l1, err := normalizeSingleLine(line1, maxAddressLineLen, "line1")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	l2, err := normalizeSingleLine(line2, maxAddressLineLen, "line2")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	cityNorm, err := normalizeSingleLine(city, maxCityLen, "city")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	regionNorm, err := normalizeSingleLine(region, maxRegionLen, "region")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	postNorm, err := normalizeSingleLine(postcode, maxPostcodeLen, "postcode")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	phoneNorm, err := normalizeSingleLine(phone, maxPhoneLen, "phone")
	if err != nil {
		return Address{}, fmt.Errorf("store address: %w", err)
	}
	return Address{
		StoreID:  storeID,
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
