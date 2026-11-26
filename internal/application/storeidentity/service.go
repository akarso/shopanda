package storeidentity

import (
	"context"
	"fmt"
	"strings"

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

// GetIdentity returns the persisted identity or an empty draft for the store.
func (s *Service) GetIdentity(ctx context.Context, storeID string) (store.Identity, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return store.Identity{}, err
	}
	id, err := s.identity.GetIdentity(ctx, storeID)
	if err != nil {
		return store.Identity{}, err
	}
	if id == nil {
		return store.Identity{StoreID: storeID}, nil
	}
	return *id, nil
}

// SaveIdentity validates and upserts identity fields.
func (s *Service) SaveIdentity(ctx context.Context, storeID string, in IdentityInput) (store.Identity, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return store.Identity{}, err
	}
	id, err := store.NewIdentity(
		storeID, in.LegalName, in.Phone, in.Hours,
		in.EmailGeneral, in.EmailSales, in.EmailSupport,
	)
	if err != nil {
		return store.Identity{}, apperror.Validation(err.Error())
	}
	if err := s.identity.SaveIdentity(ctx, id); err != nil {
		return store.Identity{}, err
	}
	return id, nil
}

// IdentityInput is the writable identity payload.
type IdentityInput struct {
	LegalName    string
	Phone        string
	Hours        string
	EmailGeneral string
	EmailSales   string
	EmailSupport string
}

// ListAddresses returns all roles with placeholders for missing rows.
// Placeholder country is the store's commerce country.
func (s *Service) ListAddresses(ctx context.Context, storeID string) ([]store.Address, error) {
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
	out := make([]store.Address, 0, len(store.AllAddressRoles()))
	for _, role := range store.AllAddressRoles() {
		if a, ok := byRole[role]; ok {
			out = append(out, a)
			continue
		}
		out = append(out, store.Address{
			StoreID: st.ID,
			Role:    role,
			Country: st.Country,
		})
	}
	return out, nil
}

// GetAddress returns one role, or a placeholder when missing.
func (s *Service) GetAddress(ctx context.Context, storeID string, role store.AddressRole) (store.Address, error) {
	st, err := s.loadStore(ctx, storeID)
	if err != nil {
		return store.Address{}, err
	}
	role, err = store.ParseAddressRole(string(role))
	if err != nil {
		return store.Address{}, apperror.Validation(err.Error())
	}
	a, err := s.identity.GetAddress(ctx, st.ID, role)
	if err != nil {
		return store.Address{}, err
	}
	if a == nil {
		return store.Address{StoreID: st.ID, Role: role, Country: st.Country}, nil
	}
	return *a, nil
}

// AddressInput is the writable address payload (role comes from the path).
type AddressInput struct {
	Line1    string
	Line2    string
	City     string
	Region   string
	Postcode string
	Country  string
	Phone    string
}

// SaveAddress validates and upserts one role address.
func (s *Service) SaveAddress(ctx context.Context, storeID string, role store.AddressRole, in AddressInput) (store.Address, error) {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return store.Address{}, err
	}
	role, err = store.ParseAddressRole(string(role))
	if err != nil {
		return store.Address{}, apperror.Validation(err.Error())
	}
	addr, err := store.NewAddress(
		storeID, role,
		in.Line1, in.Line2, in.City, in.Region, in.Postcode, in.Country, in.Phone,
	)
	if err != nil {
		return store.Address{}, apperror.Validation(err.Error())
	}
	if err := s.identity.SaveAddress(ctx, addr); err != nil {
		return store.Address{}, err
	}
	return addr, nil
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

// DeleteTaxIdentifier removes one type for the store.
func (s *Service) DeleteTaxIdentifier(ctx context.Context, storeID, typ string) error {
	storeID, err := s.requireStore(ctx, storeID)
	if err != nil {
		return err
	}
	typ = strings.ToLower(strings.TrimSpace(typ))
	if typ == "" {
		return apperror.Validation("type must not be empty")
	}
	return s.identity.DeleteTaxIdentifier(ctx, storeID, typ)
}

// ListTaxIdentifierTypes returns registry types, optionally filtered by country.
// When country is empty, returns All(). When set, returns ApplicableTo(country).
func (s *Service) ListTaxIdentifierTypes(country string) []store.TaxIdentifierType {
	country = strings.TrimSpace(country)
	if country == "" {
		return s.types.All()
	}
	return s.types.ApplicableTo(country)
}

// PublicIdentity is the storefront/headless payload (no SMTP secrets).
type PublicIdentity struct {
	LegalName    string                `json:"legal_name"`
	Phone        string                `json:"phone"`
	Hours        string                `json:"hours"`
	EmailGeneral string                `json:"email_general"`
	EmailSales   string                `json:"email_sales"`
	EmailSupport string                `json:"email_support"`
	General      *store.Address        `json:"general,omitempty"`
	Invoice      *store.Address        `json:"invoice,omitempty"`
	TaxIDs       []store.TaxIdentifier `json:"tax_identifiers"`
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
	if taxIDs == nil {
		taxIDs = []store.TaxIdentifier{}
	}
	out := PublicIdentity{TaxIDs: taxIDs}
	if id != nil {
		out.LegalName = id.LegalName
		out.Phone = id.Phone
		out.Hours = id.Hours
		out.EmailGeneral = id.EmailGeneral
		out.EmailSales = id.EmailSales
		out.EmailSupport = id.EmailSupport
	}
	if general != nil && !general.IsEmpty() {
		out.General = general
	}
	if resolvedInvoice != nil {
		out.Invoice = resolvedInvoice
	}
	return out, nil
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
