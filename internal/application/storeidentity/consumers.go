package storeidentity

import (
	"context"
	"strings"

	"github.com/akarso/shopanda/internal/domain/invoice"
	"github.com/akarso/shopanda/internal/domain/store"
)

// InvoiceSeller builds the PDF seller block for a store at invoice issue time.
// Missing identity / addresses yield an empty block (caller seals empty).
// Lookup / DB errors are returned — callers must not treat them as empty.
// Tax IDs are filtered by store.InvoiceSellerTaxType (VAT-style only).
func (s *Service) InvoiceSeller(ctx context.Context, storeID string) (invoice.SellerBlock, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return invoice.SellerBlock{}, err
	}
	id, err := s.identity.GetIdentity(ctx, storeID)
	if err != nil {
		return invoice.SellerBlock{}, err
	}
	general, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleGeneral)
	if err != nil {
		return invoice.SellerBlock{}, err
	}
	invoiceAddr, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleInvoice)
	if err != nil {
		return invoice.SellerBlock{}, err
	}
	resolved := store.ResolveInvoiceAddress(invoiceAddr, general)
	taxIDs, err := s.identity.ListTaxIdentifiers(ctx, storeID)
	if err != nil {
		return invoice.SellerBlock{}, err
	}

	out := invoice.SellerBlock{}
	if id != nil {
		out.LegalName = strings.TrimSpace(id.LegalName)
		out.Phone = strings.TrimSpace(id.Phone)
		out.Email = store.PreferContactEmail(id.EmailSales, id.EmailGeneral)
	}
	if resolved != nil {
		out.AddressLines = resolved.FormatLines()
		if out.Phone == "" {
			out.Phone = strings.TrimSpace(resolved.Phone)
		}
	}
	for _, ti := range taxIDs {
		if !store.InvoiceSellerTaxType(ti.Type) {
			continue
		}
		value := strings.TrimSpace(ti.Value)
		if value == "" {
			continue
		}
		label := ti.Type
		if t, ok := s.types.Get(ti.Type); ok && strings.TrimSpace(t.Label) != "" {
			label = t.Label
		}
		out.TaxIDs = append(out.TaxIDs, invoice.SellerTaxLine{Label: label, Value: value})
	}
	return out.SanitizeForPDF(), nil
}

// FormattedGeneralAddress returns the display string for the general address,
// or "" when missing/empty. Used by EmailData.StoreAddress and layout.
func (s *Service) FormattedGeneralAddress(ctx context.Context, storeID string) (string, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return "", err
	}
	general, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleGeneral)
	if err != nil {
		return "", err
	}
	return general.FormatDisplay(), nil
}

// LayoutIdentityFields are storefront layout imprint fields (PR-1205).
// Theme HTML that renders them ships in Track D (PR-1231).
type LayoutIdentityFields struct {
	LegalName    string
	StoreAddress string
	StorePhone   string
	TaxIDLine    string
	EmailGeneral string
	Hours        string
}

// LayoutFields builds footer/header identity fields without loading the invoice
// address (unused by layout). Prefer caching at the call site once PR-1231
// renders these fields.
func (s *Service) LayoutFields(ctx context.Context, storeID string) (LayoutIdentityFields, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return LayoutIdentityFields{}, err
	}
	id, err := s.identity.GetIdentity(ctx, storeID)
	if err != nil {
		return LayoutIdentityFields{}, err
	}
	general, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleGeneral)
	if err != nil {
		return LayoutIdentityFields{}, err
	}
	taxIDs, err := s.identity.ListTaxIdentifiers(ctx, storeID)
	if err != nil {
		return LayoutIdentityFields{}, err
	}

	out := LayoutIdentityFields{}
	if id != nil {
		out.LegalName = strings.TrimSpace(id.LegalName)
		out.StorePhone = strings.TrimSpace(id.Phone)
		out.EmailGeneral = strings.TrimSpace(id.EmailGeneral)
		out.Hours = strings.TrimSpace(id.Hours)
	}
	out.StoreAddress = general.FormatDisplay()
	lines := make([]string, 0, len(taxIDs))
	for _, ti := range taxIDs {
		if !store.ImprintPublicTaxType(ti.Type) {
			continue
		}
		label := ti.Type
		if t, ok := s.types.Get(ti.Type); ok && strings.TrimSpace(t.Label) != "" {
			label = t.Label
		}
		if line := store.FormatTaxIDLine(label, ti.Value); line != "" {
			lines = append(lines, line)
		}
	}
	out.TaxIDLine = store.JoinTaxIDLines(lines)
	return out, nil
}
