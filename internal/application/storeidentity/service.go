package storeidentity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

// Service orchestrates store identity, addresses, and tax identifier values.
type Service struct {
	identity store.IdentityRepository
	stores   store.StoreRepository
	types    *store.TaxIdentifierTypeRegistry
}

// NewService constructs a store-identity application service.
func NewService(
	identity store.IdentityRepository,
	stores store.StoreRepository,
	types *store.TaxIdentifierTypeRegistry,
) *Service {
	if identity == nil {
		panic("storeidentity.NewService: nil identity repository")
	}
	if stores == nil {
		panic("storeidentity.NewService: nil store repository")
	}
	if types == nil {
		panic("storeidentity.NewService: nil tax identifier type registry")
	}
	return &Service{identity: identity, stores: stores, types: types}
}

// IdentityView is the admin identity payload.
type IdentityView struct {
	StoreID      string     `json:"store_id"`
	LegalName    string     `json:"legal_name"`
	Phone        string     `json:"phone"`
	Hours        string     `json:"hours"`
	EmailGeneral string     `json:"email_general"`
	EmailSales   string     `json:"email_sales"`
	EmailSupport string     `json:"email_support"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
	Persisted    bool       `json:"persisted"`
}

// GetIdentity returns the persisted identity or an empty draft for the store.
func (s *Service) GetIdentity(ctx context.Context, storeID string) (IdentityView, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return IdentityView{}, err
	}
	id, err := s.identity.GetIdentity(ctx, storeID)
	if err != nil {
		return IdentityView{}, err
	}
	if id == nil {
		return IdentityView{StoreID: storeID, Persisted: false}, nil
	}
	updated := id.UpdatedAt
	return IdentityView{
		StoreID:      id.StoreID,
		LegalName:    id.LegalName,
		Phone:        id.Phone,
		Hours:        id.Hours,
		EmailGeneral: id.EmailGeneral,
		EmailSales:   id.EmailSales,
		EmailSupport: id.EmailSupport,
		UpdatedAt:    &updated,
		Persisted:    true,
	}, nil
}

// IdentityPatch is a partial identity update. Nil fields are left unchanged.
type IdentityPatch struct {
	LegalName    *string
	Phone        *string
	Hours        *string
	EmailGeneral *string
	EmailSales   *string
	EmailSupport *string
}

// HasChanges reports whether any field is present.
func (p IdentityPatch) HasChanges() bool {
	return p.LegalName != nil || p.Phone != nil || p.Hours != nil ||
		p.EmailGeneral != nil || p.EmailSales != nil || p.EmailSupport != nil
}

// PatchIdentity merges patch onto the existing row (or empty draft) and saves.
func (s *Service) PatchIdentity(ctx context.Context, storeID string, patch IdentityPatch) (IdentityView, error) {
	if !patch.HasChanges() {
		return IdentityView{}, apperror.Validation("at least one identity field is required")
	}
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return IdentityView{}, err
	}
	cur, err := s.identity.GetIdentity(ctx, storeID)
	if err != nil {
		return IdentityView{}, err
	}
	legal, phone, hours, eg, es, eu := "", "", "", "", "", ""
	if cur != nil {
		legal, phone, hours = cur.LegalName, cur.Phone, cur.Hours
		eg, es, eu = cur.EmailGeneral, cur.EmailSales, cur.EmailSupport
	}
	if patch.LegalName != nil {
		legal = *patch.LegalName
	}
	if patch.Phone != nil {
		phone = *patch.Phone
	}
	if patch.Hours != nil {
		hours = *patch.Hours
	}
	if patch.EmailGeneral != nil {
		eg = *patch.EmailGeneral
	}
	if patch.EmailSales != nil {
		es = *patch.EmailSales
	}
	if patch.EmailSupport != nil {
		eu = *patch.EmailSupport
	}
	id, err := store.NewIdentity(storeID, legal, phone, hours, eg, es, eu)
	if err != nil {
		return IdentityView{}, apperror.Validation(err.Error())
	}
	if err := s.identity.SaveIdentity(ctx, id); err != nil {
		return IdentityView{}, err
	}
	updated := id.UpdatedAt
	return IdentityView{
		StoreID:      id.StoreID,
		LegalName:    id.LegalName,
		Phone:        id.Phone,
		Hours:        id.Hours,
		EmailGeneral: id.EmailGeneral,
		EmailSales:   id.EmailSales,
		EmailSupport: id.EmailSupport,
		UpdatedAt:    &updated,
		Persisted:    true,
	}, nil
}

// AddressRecord is one admin address row (persisted or placeholder).
type AddressRecord struct {
	StoreID   string            `json:"store_id"`
	Role      store.AddressRole `json:"role"`
	Line1     string            `json:"line1"`
	Line2     string            `json:"line2"`
	City      string            `json:"city"`
	Region    string            `json:"region"`
	Postcode  string            `json:"postcode"`
	Country   string            `json:"country"`
	Phone     string            `json:"phone"`
	Persisted bool              `json:"persisted"`
}

func addressRecord(a store.Address, persisted bool) AddressRecord {
	return AddressRecord{
		StoreID:   a.StoreID,
		Role:      a.Role,
		Line1:     a.Line1,
		Line2:     a.Line2,
		City:      a.City,
		Region:    a.Region,
		Postcode:  a.Postcode,
		Country:   a.Country,
		Phone:     a.Phone,
		Persisted: persisted,
	}
}

// ListAddresses returns all roles with placeholders for missing rows.
func (s *Service) ListAddresses(ctx context.Context, storeID string) ([]AddressRecord, error) {
	st, err := s.loadStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	persisted, err := s.identity.ListAddresses(ctx, st.ID)
	if err != nil {
		return nil, err
	}
	byRole := make(map[store.AddressRole]store.Address, len(persisted))
	for _, a := range persisted {
		byRole[a.Role] = a
	}
	out := make([]AddressRecord, 0, len(store.AllAddressRoles()))
	for _, role := range store.AllAddressRoles() {
		if a, ok := byRole[role]; ok {
			out = append(out, addressRecord(a, true))
			continue
		}
		out = append(out, AddressRecord{
			StoreID:   st.ID,
			Role:      role,
			Country:   st.Country,
			Persisted: false,
		})
	}
	return out, nil
}

// GetAddress returns one role, or a placeholder when missing (no role fallback).
func (s *Service) GetAddress(ctx context.Context, storeID string, role store.AddressRole) (AddressRecord, error) {
	st, err := s.loadStore(ctx, storeID)
	if err != nil {
		return AddressRecord{}, err
	}
	role, err = store.ParseAddressRole(string(role))
	if err != nil {
		return AddressRecord{}, apperror.Validation(err.Error())
	}
	a, err := s.identity.GetAddress(ctx, st.ID, role)
	if err != nil {
		return AddressRecord{}, err
	}
	if a == nil {
		return AddressRecord{StoreID: st.ID, Role: role, Country: st.Country, Persisted: false}, nil
	}
	return addressRecord(*a, true), nil
}

// ResolveReturns loads returns → shipping_origin → general (postal content).
func (s *Service) ResolveReturns(ctx context.Context, storeID string) (*store.Address, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	returnsAddr, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleReturns)
	if err != nil {
		return nil, err
	}
	origin, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleShippingOrigin)
	if err != nil {
		return nil, err
	}
	general, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleGeneral)
	if err != nil {
		return nil, err
	}
	resolved := store.ResolveReturnsAddress(returnsAddr, origin, general)
	return cloneAddress(resolved), nil
}

// AddressPatch is a partial address update. Nil fields are left unchanged.
type AddressPatch struct {
	Line1    *string
	Line2    *string
	City     *string
	Region   *string
	Postcode *string
	Country  *string
	Phone    *string
}

// HasChanges reports whether any field is present.
func (p AddressPatch) HasChanges() bool {
	return p.Line1 != nil || p.Line2 != nil || p.City != nil || p.Region != nil ||
		p.Postcode != nil || p.Country != nil || p.Phone != nil
}

// PatchAddress merges patch onto the existing role row (or store-country draft).
func (s *Service) PatchAddress(ctx context.Context, storeID string, role store.AddressRole, patch AddressPatch) (AddressRecord, error) {
	if !patch.HasChanges() {
		return AddressRecord{}, apperror.Validation("at least one address field is required")
	}
	st, err := s.loadStore(ctx, storeID)
	if err != nil {
		return AddressRecord{}, err
	}
	role, err = store.ParseAddressRole(string(role))
	if err != nil {
		return AddressRecord{}, apperror.Validation(err.Error())
	}
	cur, err := s.identity.GetAddress(ctx, st.ID, role)
	if err != nil {
		return AddressRecord{}, err
	}
	line1, line2, city, region, postcode, country, phone := "", "", "", "", "", st.Country, ""
	if cur != nil {
		line1, line2, city = cur.Line1, cur.Line2, cur.City
		region, postcode, country, phone = cur.Region, cur.Postcode, cur.Country, cur.Phone
	}
	if patch.Line1 != nil {
		line1 = *patch.Line1
	}
	if patch.Line2 != nil {
		line2 = *patch.Line2
	}
	if patch.City != nil {
		city = *patch.City
	}
	if patch.Region != nil {
		region = *patch.Region
	}
	if patch.Postcode != nil {
		postcode = *patch.Postcode
	}
	if patch.Country != nil {
		country = *patch.Country
	}
	if patch.Phone != nil {
		phone = *patch.Phone
	}
	addr, err := store.NewAddress(st.ID, role, line1, line2, city, region, postcode, country, phone)
	if err != nil {
		return AddressRecord{}, apperror.Validation(err.Error())
	}
	if err := s.identity.SaveAddress(ctx, addr); err != nil {
		return AddressRecord{}, err
	}
	return addressRecord(addr, true), nil
}

// ListTaxIdentifiers returns every tax ID for the store.
func (s *Service) ListTaxIdentifiers(ctx context.Context, storeID string) ([]store.TaxIdentifier, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	return s.identity.ListTaxIdentifiers(ctx, storeID)
}

// UpsertTaxIdentifier validates type against the registry and upserts.
func (s *Service) UpsertTaxIdentifier(ctx context.Context, storeID, typ, value string) (store.TaxIdentifier, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return store.TaxIdentifier{}, err
	}
	ti, err := store.NewTaxIdentifier(storeID, typ, value)
	if err != nil {
		return store.TaxIdentifier{}, apperror.Validation(err.Error())
	}
	if _, ok := s.types.Get(ti.Type); !ok {
		return store.TaxIdentifier{}, apperror.Validation(fmt.Sprintf("unknown tax identifier type %q", ti.Type))
	}
	if err := s.identity.UpsertTaxIdentifier(ctx, ti); err != nil {
		return store.TaxIdentifier{}, err
	}
	return ti, nil
}

// DeleteTaxIdentifier removes one type for the store (NotFound if missing).
func (s *Service) DeleteTaxIdentifier(ctx context.Context, storeID, typ string) error {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return err
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return apperror.Validation("type must not be empty")
	}
	if !taxTypeTokenOK(typ) {
		return apperror.Validation("type must be a snake token (e.g. pl_nip)")
	}
	return s.identity.DeleteTaxIdentifier(ctx, storeID, typ)
}

func taxTypeTokenOK(typ string) bool {
	if typ == "" || typ[0] < 'a' || typ[0] > 'z' {
		return false
	}
	for i := 1; i < len(typ); i++ {
		c := typ[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

// ListTaxIdentifierTypes returns registry types, optionally filtered by country.
// Empty country returns All(). Invalid country is a validation error.
func (s *Service) ListTaxIdentifierTypes(country string) ([]store.TaxIdentifierType, error) {
	country = strings.TrimSpace(country)
	if country == "" {
		return s.types.All(), nil
	}
	if _, err := store.NormalizeCountry(country); err != nil {
		return nil, apperror.Validation(err.Error())
	}
	return s.types.ApplicableTo(country), nil
}

// PublicAddress is the storefront address DTO (no store_id).
type PublicAddress struct {
	Role     store.AddressRole `json:"role"`
	Line1    string            `json:"line1"`
	Line2    string            `json:"line2"`
	City     string            `json:"city"`
	Region   string            `json:"region"`
	Postcode string            `json:"postcode"`
	Country  string            `json:"country"`
	Phone    string            `json:"phone"`
}

// PublicTaxID is an imprint-safe tax identifier (no store_id).
type PublicTaxID struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// PublicIdentity is the storefront/headless payload (no SMTP secrets).
type PublicIdentity struct {
	LegalName    string         `json:"legal_name"`
	Phone        string         `json:"phone"`
	Hours        string         `json:"hours"`
	EmailGeneral string         `json:"email_general"`
	EmailSales   string         `json:"email_sales"`
	EmailSupport string         `json:"email_support"`
	General      *PublicAddress `json:"general,omitempty"`
	Invoice      *PublicAddress `json:"invoice,omitempty"`
	TaxIDs       []PublicTaxID  `json:"tax_identifiers"`
}

// GetPublicIdentity builds the public view for the current store.
func (s *Service) GetPublicIdentity(ctx context.Context, storeID string) (PublicIdentity, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return PublicIdentity{}, err
	}
	id, err := s.identity.GetIdentity(ctx, storeID)
	if err != nil {
		return PublicIdentity{}, err
	}
	general, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleGeneral)
	if err != nil {
		return PublicIdentity{}, err
	}
	invoice, err := s.identity.GetAddress(ctx, storeID, store.AddressRoleInvoice)
	if err != nil {
		return PublicIdentity{}, err
	}
	resolvedInvoice := store.ResolveInvoiceAddress(invoice, general)
	taxIDs, err := s.identity.ListTaxIdentifiers(ctx, storeID)
	if err != nil {
		return PublicIdentity{}, err
	}
	out := PublicIdentity{TaxIDs: make([]PublicTaxID, 0)}
	if id != nil {
		out.LegalName = id.LegalName
		out.Phone = id.Phone
		out.Hours = id.Hours
		out.EmailGeneral = id.EmailGeneral
		out.EmailSales = id.EmailSales
		out.EmailSupport = id.EmailSupport
	}
	if general != nil && !general.IsEmpty() {
		out.General = toPublicAddress(general, store.AddressRoleGeneral)
	}
	if resolvedInvoice != nil {
		// Always advertise role=invoice on a copy (even when falling back to general).
		out.Invoice = toPublicAddress(resolvedInvoice, store.AddressRoleInvoice)
	}
	for _, ti := range taxIDs {
		if !store.ImprintPublicTaxType(ti.Type) {
			continue
		}
		out.TaxIDs = append(out.TaxIDs, PublicTaxID{Type: ti.Type, Value: ti.Value})
	}
	return out, nil
}

func toPublicAddress(a *store.Address, role store.AddressRole) *PublicAddress {
	if a == nil {
		return nil
	}
	return &PublicAddress{
		Role:     role,
		Line1:    a.Line1,
		Line2:    a.Line2,
		City:     a.City,
		Region:   a.Region,
		Postcode: a.Postcode,
		Country:  a.Country,
		Phone:    a.Phone,
	}
}

func cloneAddress(a *store.Address) *store.Address {
	if a == nil {
		return nil
	}
	cp := *a
	return &cp
}

func (s *Service) requireStore(ctx context.Context, storeID string) (string, error) {
	st, err := s.loadStore(ctx, storeID)
	if err != nil {
		return "", err
	}
	return st.ID, nil
}

func (s *Service) loadStore(ctx context.Context, storeID string) (*store.Store, error) {
	storeID = strings.TrimSpace(storeID)
	if storeID == "" {
		return nil, apperror.Validation("store id must not be empty")
	}
	st, err := s.stores.FindByID(ctx, storeID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, apperror.NotFound("store not found")
	}
	return st, nil
}
