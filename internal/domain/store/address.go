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

// AddressRoles lists every valid role in stable order (admin index / public GET).
var AddressRoles = []AddressRole{
	AddressRoleGeneral,
	AddressRoleInvoice,
	AddressRoleShippingOrigin,
	AddressRoleReturns,
}

// ParseAddressRole normalizes and validates a role string.
func ParseAddressRole(v string) (AddressRole, error) {
	r := AddressRole(strings.TrimSpace(strings.ToLower(v)))
	switch r {
	case AddressRoleGeneral, AddressRoleInvoice, AddressRoleShippingOrigin, AddressRoleReturns:
		return r, nil
	default:
		return "", fmt.Errorf("store address: invalid role %q", v)
	}
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
	Country  string // ISO 3166-1 alpha-2
	Phone    string
}

// NewAddress constructs a role address. Country must be a 2-letter ISO code.
// Other fields may be empty (admin placeholders); role and storeID are required.
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
	return Address{
		StoreID:  storeID,
		Role:     role,
		Line1:    strings.TrimSpace(line1),
		Line2:    strings.TrimSpace(line2),
		City:     strings.TrimSpace(city),
		Region:   strings.TrimSpace(region),
		Postcode: strings.TrimSpace(postcode),
		Country:  cty,
		Phone:    strings.TrimSpace(phone),
	}, nil
}

// NewAddressFromDB reconstructs Address from persistence (no re-validation).
func NewAddressFromDB(
	storeID string,
	role AddressRole,
	line1, line2, city, region, postcode, country, phone string,
) Address {
	return Address{
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
