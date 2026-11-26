package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type identityMemRepo struct {
	identities map[string]store.Identity
	addresses  map[string]store.Address
	tax        map[string]store.TaxIdentifier
}

func newIdentityMem() *identityMemRepo {
	return &identityMemRepo{
		identities: map[string]store.Identity{},
		addresses:  map[string]store.Address{},
		tax:        map[string]store.TaxIdentifier{},
	}
}

func (m *identityMemRepo) GetIdentity(_ context.Context, storeID string) (*store.Identity, error) {
	if v, ok := m.identities[storeID]; ok {
		cp := v
		return &cp, nil
	}
	return nil, nil
}
func (m *identityMemRepo) SaveIdentity(_ context.Context, id store.Identity) error {
	m.identities[id.StoreID] = id
	return nil
}
func (m *identityMemRepo) MutateIdentity(ctx context.Context, storeID string, fn func(current *store.Identity) (store.Identity, error)) (store.Identity, error) {
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
func (m *identityMemRepo) GetAddress(_ context.Context, storeID string, role store.AddressRole) (*store.Address, error) {
	if a, ok := m.addresses[storeID+"|"+string(role)]; ok {
		cp := a
		return &cp, nil
	}
	return nil, nil
}
func (m *identityMemRepo) ListAddresses(_ context.Context, storeID string) ([]store.Address, error) {
	out := []store.Address{}
	for _, a := range m.addresses {
		if a.StoreID == storeID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *identityMemRepo) SaveAddress(_ context.Context, addr store.Address) error {
	m.addresses[addr.StoreID+"|"+string(addr.Role)] = addr
	return nil
}
func (m *identityMemRepo) MutateAddress(ctx context.Context, storeID string, role store.AddressRole, fn func(current *store.Address) (store.Address, error)) (store.Address, error) {
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
func (m *identityMemRepo) ListTaxIdentifiers(_ context.Context, storeID string) ([]store.TaxIdentifier, error) {
	out := []store.TaxIdentifier{}
	for _, ti := range m.tax {
		if ti.StoreID == storeID {
			out = append(out, ti)
		}
	}
	return out, nil
}
func (m *identityMemRepo) UpsertTaxIdentifier(_ context.Context, ti store.TaxIdentifier) error {
	m.tax[ti.StoreID+"|"+ti.Type] = ti
	return nil
}
func (m *identityMemRepo) DeleteTaxIdentifier(_ context.Context, storeID, typ string) error {
	key := storeID + "|" + typ
	if _, ok := m.tax[key]; !ok {
		return apperror.NotFound("tax identifier not found")
	}
	delete(m.tax, key)
	return nil
}

type identityStoreRepo struct {
	st *store.Store
}

func (r *identityStoreRepo) FindByID(_ context.Context, id string) (*store.Store, error) {
	if r.st != nil && r.st.ID == id {
		return r.st, nil
	}
	return nil, nil
}
func (r *identityStoreRepo) FindByCode(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (r *identityStoreRepo) FindByDomain(context.Context, string) (*store.Store, error) {
	return nil, nil
}
func (r *identityStoreRepo) FindDefault(context.Context) (*store.Store, error) { return nil, nil }
func (r *identityStoreRepo) FindAll(context.Context) ([]store.Store, error)    { return nil, nil }
func (r *identityStoreRepo) Create(context.Context, *store.Store) error        { return nil }
func (r *identityStoreRepo) Update(context.Context, *store.Store) error        { return nil }

func identityTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.NewStore(id.New(), "pl", "PL", "PLN", "PL", "pl", "pl.test")
	if err != nil {
		t.Fatal(err)
	}
	return &s
}

func withAdminStore(r *http.Request, storeID string) *http.Request {
	return r.WithContext((&adminapp.AdminContext{StoreID: storeID}).WithContext(r.Context()))
}

func TestStoreIdentityAdmin_UnknownRole422(t *testing.T) {
	st := identityTestStore(t)
	svc := storeidentity.NewService(newIdentityMem(), &identityStoreRepo{st: st}, store.DefaultTaxIdentifierTypeRegistry())
	h := admin.NewStoreIdentityAdminHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("PUT /api/v1/admin/store-addresses/{role}", h.PutAddress())

	body := []byte(`{"country":"PL","line1":"x"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/store-addresses/warehouse", bytes.NewReader(body))
	req = withAdminStore(req, st.ID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestStoreIdentityAdmin_PutPartialAndUnknownField(t *testing.T) {
	st := identityTestStore(t)
	svc := storeidentity.NewService(newIdentityMem(), &identityStoreRepo{st: st}, store.DefaultTaxIdentifierTypeRegistry())
	h := admin.NewStoreIdentityAdminHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("PUT /api/v1/admin/store-identity", h.PutIdentity())
	mux.Handle("GET /api/v1/admin/store-identity", h.GetIdentity())

	body := []byte(`{"legal_name":"Acme","email_general":"a@b.test","phone":"+481"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/store-identity", bytes.NewReader(body))
	req = withAdminStore(req, st.ID)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rr.Code, rr.Body.String())
	}

	body = []byte(`{"legal_name":"Acme Sp. z o.o."}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/store-identity", bytes.NewReader(body))
	req = withAdminStore(req, st.ID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("partial put status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/store-identity", nil)
	req = withAdminStore(req, st.ID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	var envelope struct {
		Data storeidentity.IdentityView `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	got := envelope.Data
	if got.LegalName != "Acme Sp. z o.o." || got.EmailGeneral != "a@b.test" || got.Phone != "+481" {
		t.Fatalf("%#v body=%s", got, rr.Body.String())
	}
	if !got.Persisted || got.UpdatedAt == nil {
		t.Fatalf("persisted markers %#v", got)
	}

	body = []byte(`{"legalName":"Nope"}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/store-identity", bytes.NewReader(body))
	req = withAdminStore(req, st.ID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown field status=%d", rr.Code)
	}

	body = []byte(`{"legal_name":null}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/store-identity", bytes.NewReader(body))
	req = withAdminStore(req, st.ID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("null clear status=%d body=%s", rr.Code, rr.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/store-identity", nil)
	req = withAdminStore(req, st.ID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.LegalName != "" || envelope.Data.EmailGeneral != "a@b.test" {
		t.Fatalf("null clear %#v", envelope.Data)
	}

	body = []byte(`{"phone":"+482"}{"phone":"+483"}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/store-identity", bytes.NewReader(body))
	req = withAdminStore(req, st.ID)
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("trailing JSON status=%d", rr.Code)
	}
}
