package storeidentity_test

import (
	"context"
	"strings"
	"testing"

	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type memIdentityRepo struct {
	identities map[string]store.Identity
	addresses  map[string]store.Address
	tax        map[string]store.TaxIdentifier
}

func newMemIdentity() *memIdentityRepo {
	return &memIdentityRepo{
		identities: map[string]store.Identity{},
		addresses:  map[string]store.Address{},
		tax:        map[string]store.TaxIdentifier{},
	}
}

func addrKey(storeID string, role store.AddressRole) string {
	return storeID + "|" + string(role)
}

func (m *memIdentityRepo) GetIdentity(_ context.Context, storeID string) (*store.Identity, error) {
	if id, ok := m.identities[storeID]; ok {
		cp := id
		return &cp, nil
	}
	return nil, nil
}

func (m *memIdentityRepo) SaveIdentity(_ context.Context, id store.Identity) error {
	m.identities[id.StoreID] = id
	return nil
}

func (m *memIdentityRepo) MutateIdentity(ctx context.Context, storeID string, fn func(current *store.Identity) (store.Identity, error)) (store.Identity, error) {
	cur, err := m.GetIdentity(ctx, storeID)
	if err != nil {
		return store.Identity{}, err
	}
	next, err := fn(cur)
	if err != nil {
		return store.Identity{}, err
	}
	if err := m.SaveIdentity(ctx, next); err != nil {
		return store.Identity{}, err
	}
	return next, nil
}

func (m *memIdentityRepo) GetAddress(_ context.Context, storeID string, role store.AddressRole) (*store.Address, error) {
	if a, ok := m.addresses[addrKey(storeID, role)]; ok {
		cp := a
		return &cp, nil
	}
	return nil, nil
}

func (m *memIdentityRepo) ListAddresses(_ context.Context, storeID string) ([]store.Address, error) {
	out := make([]store.Address, 0)
	for _, a := range m.addresses {
		if a.StoreID == storeID {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memIdentityRepo) SaveAddress(_ context.Context, addr store.Address) error {
	m.addresses[addrKey(addr.StoreID, addr.Role)] = addr
	return nil
}

func (m *memIdentityRepo) MutateAddress(ctx context.Context, storeID string, role store.AddressRole, fn func(current *store.Address) (store.Address, error)) (store.Address, error) {
	cur, err := m.GetAddress(ctx, storeID, role)
	if err != nil {
		return store.Address{}, err
	}
	next, err := fn(cur)
	if err != nil {
		return store.Address{}, err
	}
	if err := m.SaveAddress(ctx, next); err != nil {
		return store.Address{}, err
	}
	return next, nil
}

func (m *memIdentityRepo) ListTaxIdentifiers(_ context.Context, storeID string) ([]store.TaxIdentifier, error) {
	out := make([]store.TaxIdentifier, 0)
	for _, ti := range m.tax {
		if ti.StoreID == storeID {
			out = append(out, ti)
		}
	}
	return out, nil
}

func (m *memIdentityRepo) UpsertTaxIdentifier(_ context.Context, ti store.TaxIdentifier) error {
	m.tax[ti.StoreID+"|"+ti.Type] = ti
	return nil
}

func (m *memIdentityRepo) DeleteTaxIdentifier(_ context.Context, storeID, typ string) error {
	key := storeID + "|" + typ
	if _, ok := m.tax[key]; !ok {
		return apperror.NotFound("tax identifier not found")
	}
	delete(m.tax, key)
	return nil
}

type memStoreRepo struct {
	byID map[string]*store.Store
}

func (m *memStoreRepo) FindByID(_ context.Context, id string) (*store.Store, error) {
	return m.byID[id], nil
}
func (m *memStoreRepo) FindByCode(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (m *memStoreRepo) FindByDomain(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (m *memStoreRepo) FindDefault(context.Context) (*store.Store, error) { return nil, nil }
func (m *memStoreRepo) FindAll(context.Context) ([]store.Store, error)    { return nil, nil }
func (m *memStoreRepo) Create(context.Context, *store.Store) error        { return nil }
func (m *memStoreRepo) Update(context.Context, *store.Store) error        { return nil }

func testStore() *store.Store {
	s, err := store.NewStore(id.New(), "pl", "PL Shop", "PLN", "PL", "pl", "pl.example.com")
	if err != nil {
		panic(err)
	}
	return &s
}

func opt(s string) storeidentity.OptionalString {
	return storeidentity.OptionalString{Set: true, Value: s}
}

func TestService_ListAddressesPlaceholders(t *testing.T) {
	st := testStore()
	svc := storeidentity.NewService(
		newMemIdentity(),
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	addrs, err := svc.ListAddresses(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 4 {
		t.Fatalf("len=%d", len(addrs))
	}
	for _, a := range addrs {
		if a.Persisted {
			t.Fatalf("want placeholder, got persisted %#v", a)
		}
		if a.Country != "PL" {
			t.Fatalf("placeholder country %q", a.Country)
		}
	}
}

func TestService_UnknownRole(t *testing.T) {
	st := testStore()
	svc := storeidentity.NewService(
		newMemIdentity(),
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	_, err := svc.PatchAddress(context.Background(), st.ID, store.AddressRole("warehouse"), storeidentity.AddressPatch{
		Country: opt("PL"),
	})
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err=%v", err)
	}
}

func TestService_PatchIdentityOmitsUnchanged(t *testing.T) {
	st := testStore()
	mem := newMemIdentity()
	svc := storeidentity.NewService(mem, &memStoreRepo{byID: map[string]*store.Store{st.ID: st}}, store.DefaultTaxIdentifierTypeRegistry())
	_, err := svc.PatchIdentity(context.Background(), st.ID, storeidentity.IdentityPatch{
		LegalName:    opt("Acme"),
		Phone:        opt("+48111"),
		EmailGeneral: opt("a@b.test"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.PatchIdentity(context.Background(), st.ID, storeidentity.IdentityPatch{
		LegalName: opt("Acme Sp. z o.o."),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.LegalName != "Acme Sp. z o.o." || got.Phone != "+48111" || got.EmailGeneral != "a@b.test" {
		t.Fatalf("%#v", got)
	}
	_, err = svc.PatchIdentity(context.Background(), st.ID, storeidentity.IdentityPatch{})
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty patch err=%v", err)
	}
	got, err = svc.PatchIdentity(context.Background(), st.ID, storeidentity.IdentityPatch{
		Phone: opt(""), // clear
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Phone != "" || got.LegalName != "Acme Sp. z o.o." {
		t.Fatalf("clear phone %#v", got)
	}
}

func TestService_PublicIdentityInvoiceFallbackAndTaxFilter(t *testing.T) {
	st := testStore()
	mem := newMemIdentity()
	idty, _ := store.NewIdentity(st.ID, "Legal Co", "+48", "9-17", "a@b.test", "s@b.test", "h@b.test")
	_ = mem.SaveIdentity(context.Background(), idty)
	addr, _ := store.NewAddress(st.ID, store.AddressRoleGeneral, "Street 1", "", "City", "", "00-001", "PL", "")
	_ = mem.SaveAddress(context.Background(), addr)
	phoneOnlyInvoice, _ := store.NewAddress(st.ID, store.AddressRoleInvoice, "", "", "", "", "", "PL", "+48999")
	_ = mem.SaveAddress(context.Background(), phoneOnlyInvoice)
	nip, _ := store.NewTaxIdentifier(st.ID, "pl_nip", "1234567890")
	_ = mem.UpsertTaxIdentifier(context.Background(), nip)
	ein, _ := store.NewTaxIdentifier(st.ID, "us_ein", "12-3456789")
	_ = mem.UpsertTaxIdentifier(context.Background(), ein)
	steuernummer, _ := store.NewTaxIdentifier(st.ID, "de_steuernummer", "123/456")
	_ = mem.UpsertTaxIdentifier(context.Background(), steuernummer)

	svc := storeidentity.NewService(
		mem,
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	view, err := svc.GetPublicIdentity(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Invoice == nil || view.Invoice.Role != store.AddressRoleInvoice || view.Invoice.Line1 != "Street 1" {
		t.Fatalf("invoice fallback %#v", view.Invoice)
	}
	if view.General != nil && view.General == view.Invoice {
		t.Fatal("general and invoice must be distinct pointers")
	}
	if len(view.TaxIDs) != 1 || view.TaxIDs[0].Type != "pl_nip" {
		t.Fatalf("tax filter %#v", view.TaxIDs)
	}
}

func TestService_ResolveReturns(t *testing.T) {
	st := testStore()
	mem := newMemIdentity()
	general, _ := store.NewAddress(st.ID, store.AddressRoleGeneral, "Main", "", "City", "", "00-001", "PL", "")
	origin, _ := store.NewAddress(st.ID, store.AddressRoleShippingOrigin, "Warehouse", "", "City", "", "00-002", "PL", "")
	_ = mem.SaveAddress(context.Background(), general)
	_ = mem.SaveAddress(context.Background(), origin)
	svc := storeidentity.NewService(mem, &memStoreRepo{byID: map[string]*store.Store{st.ID: st}}, store.DefaultTaxIdentifierTypeRegistry())
	got, err := svc.ResolveReturns(context.Background(), st.ID)
	if err != nil || got == nil || got.Line1 != "Warehouse" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestService_UpsertUnknownType(t *testing.T) {
	st := testStore()
	svc := storeidentity.NewService(
		newMemIdentity(),
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	_, err := svc.UpsertTaxIdentifier(context.Background(), st.ID, "not_a_real_type", "x")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err=%v", err)
	}
}

func TestService_DeleteTaxUnknown(t *testing.T) {
	st := testStore()
	svc := storeidentity.NewService(
		newMemIdentity(),
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	err := svc.DeleteTaxIdentifier(context.Background(), st.ID, "pl_nip")
	if err == nil || !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("err=%v", err)
	}
	err = svc.DeleteTaxIdentifier(context.Background(), st.ID, "BAD TYPE")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("bad token err=%v", err)
	}
}

func TestService_ListTaxIdentifierTypesInvalidCountry(t *testing.T) {
	st := testStore()
	svc := storeidentity.NewService(
		newMemIdentity(),
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	_, err := svc.ListTaxIdentifierTypes("12")
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err=%v", err)
	}
	_, err = svc.ListTaxIdentifierTypes("uk")
	if err != nil {
		t.Fatal(err)
	}
}

func TestService_GetIdentityDraftOmitsUpdatedAt(t *testing.T) {
	st := testStore()
	svc := storeidentity.NewService(
		newMemIdentity(),
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	got, err := svc.GetIdentity(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Persisted || got.UpdatedAt != nil {
		t.Fatalf("%#v", got)
	}
}

func TestImprintPublicTaxType(t *testing.T) {
	if !store.ImprintPublicTaxType("pl_nip") {
		t.Fatal("pl_nip should be public")
	}
	if store.ImprintPublicTaxType("us_ein") {
		t.Fatal("us_ein should be admin-only")
	}
	if store.ImprintPublicTaxType("de_steuernummer") {
		t.Fatal("steuernummer should be admin-only")
	}
	core := map[string]struct{}{}
	for _, c := range store.CoreTaxIdentifierTypeCodes() {
		core[c] = struct{}{}
	}
	for _, code := range []string{
		"eu_vat", "pl_nip", "de_ustidnr", "fr_tva", "it_partita_iva", "es_nif", "nl_btw", "uk_vat",
	} {
		if _, ok := core[code]; !ok {
			t.Fatalf("imprint code %q missing from CoreTaxIdentifierTypeCodes", code)
		}
		if !store.ImprintPublicTaxType(code) {
			t.Fatalf("%q should be imprint-public", code)
		}
	}
}

func TestService_InvoiceSellerAndLayoutFields(t *testing.T) {
	st := testStore()
	mem := newMemIdentity()
	idty, _ := store.NewIdentity(st.ID, "Legal Co", "+48111", "9-17", "general@x.test", "sales@x.test", "")
	_ = mem.SaveIdentity(context.Background(), idty)
	general, _ := store.NewAddress(st.ID, store.AddressRoleGeneral, "Street 1", "Apt 2", "City", "Region", "00-001", "PL", "")
	_ = mem.SaveAddress(context.Background(), general)
	nip, _ := store.NewTaxIdentifier(st.ID, "pl_nip", "5252344022")
	_ = mem.UpsertTaxIdentifier(context.Background(), nip)
	ein, _ := store.NewTaxIdentifier(st.ID, "us_ein", "12-3456789")
	_ = mem.UpsertTaxIdentifier(context.Background(), ein)

	svc := storeidentity.NewService(
		mem,
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)

	seller, err := svc.InvoiceSeller(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seller.LegalName != "Legal Co" || seller.Email != "sales@x.test" {
		t.Fatalf("seller %#v", seller)
	}
	if len(seller.TaxIDs) != 1 || seller.TaxIDs[0].Label != "NIP" || seller.TaxIDs[0].Value != "5252344022" {
		t.Fatalf("invoice seller must be VAT-style only, got %#v", seller.TaxIDs)
	}

	// Invoice address wins over general when it has postal content.
	invoiceAddr, _ := store.NewAddress(st.ID, store.AddressRoleInvoice, "Invoice Rd", "", "Krakow", "", "30-001", "PL", "")
	_ = mem.SaveAddress(context.Background(), invoiceAddr)
	seller, err = svc.InvoiceSeller(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(seller.AddressLines) == 0 || seller.AddressLines[0] != "Invoice Rd" {
		t.Fatalf("prefer invoice address %#v", seller.AddressLines)
	}
	seller.Email = "" // ensure PreferContactEmail covered above already
	idty2, _ := store.NewIdentity(st.ID, "Legal Co", "+48111", "9-17", "general@x.test", "", "")
	_ = mem.SaveIdentity(context.Background(), idty2)
	seller, err = svc.InvoiceSeller(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seller.Email != "general@x.test" {
		t.Fatalf("sales empty → general email %#v", seller)
	}

	addr, err := svc.FormattedGeneralAddress(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(addr, "Street 1") || !strings.Contains(addr, "00-001 City") {
		t.Fatalf("formatted address %q", addr)
	}

	layout, err := svc.LayoutFields(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if layout.LegalName != "Legal Co" || layout.EmailGeneral != "general@x.test" {
		t.Fatalf("layout %#v", layout)
	}
	if !strings.Contains(layout.TaxIDLine, "NIP: 5252344022") {
		t.Fatalf("tax line %q", layout.TaxIDLine)
	}
	if strings.Contains(layout.TaxIDLine, "EIN") || strings.Contains(layout.TaxIDLine, "12-3456789") {
		t.Fatalf("layout must filter imprint-public only: %q", layout.TaxIDLine)
	}
}
