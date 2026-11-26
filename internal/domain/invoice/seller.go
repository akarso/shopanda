package invoice

import "strings"

// SellerTaxLine is one tax identifier printed on the invoice seller block.
type SellerTaxLine struct {
	Label string
	Value string
}

// SellerBlock is the merchant identity printed above invoice metadata.
// An empty block omits the seller section entirely.
type SellerBlock struct {
	LegalName    string
	AddressLines []string
	Phone        string
	Email        string
	TaxIDs       []SellerTaxLine
}

// IsEmpty reports whether the block has nothing to print.
func (s SellerBlock) IsEmpty() bool {
	if strings.TrimSpace(s.LegalName) != "" {
		return false
	}
	if strings.TrimSpace(s.Phone) != "" || strings.TrimSpace(s.Email) != "" {
		return false
	}
	for _, line := range s.AddressLines {
		if strings.TrimSpace(line) != "" {
			return false
		}
	}
	for _, t := range s.TaxIDs {
		if strings.TrimSpace(t.Value) != "" {
			return false
		}
	}
	return true
}
