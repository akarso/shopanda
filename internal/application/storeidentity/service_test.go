package storeidentity_test

import (
	"context"
	"testing"

	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type memIdentityRepo struct {
	identities map[string]store.Identity
	addresses  map[string]store.Address // key storeID|role
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
	delete(m.tax, storeID+"|"+typ)
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
	_, err := svc.SaveAddress(context.Background(), st.ID, store.AddressRole("warehouse"), storeidentity.AddressInput{
		Country: "PL",
	})
	if err == nil || !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("err=%v", err)
	}
}

func TestService_PublicIdentityNoSMTP(t *testing.T) {
	st := testStore()
	mem := newMemIdentity()
	idty, _ := store.NewIdentity(st.ID, "Legal Co", "+48", "9-17", "a@b.test", "s@b.test", "h@b.test")
	_ = mem.SaveIdentity(context.Background(), idty)
	addr, _ := store.NewAddress(st.ID, store.AddressRoleGeneral, "Street 1", "", "City", "", "00-001", "PL", "")
	_ = mem.SaveAddress(context.Background(), addr)

	svc := storeidentity.NewService(
		mem,
		&memStoreRepo{byID: map[string]*store.Store{st.ID: st}},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	view, err := svc.GetPublicIdentity(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.LegalName != "Legal Co" || view.General == nil {
		t.Fatalf("%#v", view)
	}
	if view.EmailGeneral != "a@b.test" {
		t.Fatalf("email %q", view.EmailGeneral)
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
