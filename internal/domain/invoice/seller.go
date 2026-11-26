package invoice

import "strings"

// SellerTaxLine is one tax identifier sealed on the invoice seller snapshot.
type SellerTaxLine struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SellerBlock is the merchant identity sealed onto an invoice at issue time.
// Render and admin re-download must use this snapshot — never live identity.
// An empty block omits the seller section entirely.
type SellerBlock struct {
	LegalName    string          `json:"legal_name,omitempty"`
	AddressLines []string        `json:"address_lines,omitempty"`
	Phone        string          `json:"phone,omitempty"`
	Email        string          `json:"email,omitempty"`
	TaxIDs       []SellerTaxLine `json:"tax_ids,omitempty"`
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

// SanitizeForPDF collapses CR/LF/TAB in seller strings so PDF string literals
// cannot split mid-token (DB rows may retain controls that API constructors reject).
func (s SellerBlock) SanitizeForPDF() SellerBlock {
	out := SellerBlock{
		LegalName: sanitizePDFLine(s.LegalName),
		Phone:     sanitizePDFLine(s.Phone),
		Email:     sanitizePDFLine(s.Email),
	}
	if len(s.AddressLines) > 0 {
		out.AddressLines = make([]string, 0, len(s.AddressLines))
		for _, line := range s.AddressLines {
			if v := sanitizePDFLine(line); v != "" {
				out.AddressLines = append(out.AddressLines, v)
			}
		}
	}
	if len(s.TaxIDs) > 0 {
		out.TaxIDs = make([]SellerTaxLine, 0, len(s.TaxIDs))
		for _, t := range s.TaxIDs {
			label := sanitizePDFLine(t.Label)
			value := sanitizePDFLine(t.Value)
			if value == "" {
				continue
			}
			out.TaxIDs = append(out.TaxIDs, SellerTaxLine{Label: label, Value: value})
		}
	}
	return out
}

func sanitizePDFLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.TrimSpace(s)
}
