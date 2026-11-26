package storefront_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	storeidentity "github.com/akarso/shopanda/internal/application/storeidentity"
	"github.com/akarso/shopanda/internal/domain/store"
	"github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/id"
)

type pubIdentityRepo struct {
	id   *store.Identity
	addr *store.Address
}

func (m *pubIdentityRepo) GetIdentity(context.Context, string) (*store.Identity, error) {
	return m.id, nil
}
func (m *pubIdentityRepo) SaveIdentity(context.Context, store.Identity) error { return nil }
func (m *pubIdentityRepo) GetAddress(_ context.Context, _ string, role store.AddressRole) (*store.Address, error) {
	if role == store.AddressRoleGeneral {
		return m.addr, nil
	}
	return nil, nil
}
func (m *pubIdentityRepo) ListAddresses(context.Context, string) ([]store.Address, error) {
	return nil, nil
}
func (m *pubIdentityRepo) SaveAddress(context.Context, store.Address) error { return nil }
func (m *pubIdentityRepo) ListTaxIdentifiers(context.Context, string) ([]store.TaxIdentifier, error) {
	return []store.TaxIdentifier{}, nil
}
func (m *pubIdentityRepo) UpsertTaxIdentifier(context.Context, store.TaxIdentifier) error {
	return nil
}
func (m *pubIdentityRepo) DeleteTaxIdentifier(context.Context, string, string) error { return nil }

type pubStoreRepo struct{ st *store.Store }

func (r *pubStoreRepo) FindByID(_ context.Context, id string) (*store.Store, error) {
	if r.st != nil && r.st.ID == id {
		return r.st, nil
	}
	return nil, nil
}
func (r *pubStoreRepo) FindByCode(context.Context, string) (*store.Store, error)  { return nil, nil }
func (r *pubStoreRepo) FindByDomain(context.Context, string) (*store.Store, error) { return nil, nil }
func (r *pubStoreRepo) FindDefault(context.Context) (*store.Store, error)          { return nil, nil }
func (r *pubStoreRepo) FindAll(context.Context) ([]store.Store, error)             { return nil, nil }
func (r *pubStoreRepo) Create(context.Context, *store.Store) error                 { return nil }
func (r *pubStoreRepo) Update(context.Context, *store.Store) error                 { return nil }

func TestStoreIdentityHandler_PublicGet(t *testing.T) {
	st, err := store.NewStore(id.New(), "pl", "PL", "PLN", "PL", "pl", "pl.test")
	if err != nil {
		t.Fatal(err)
	}
	idty, _ := store.NewIdentity(st.ID, "Legal", "", "", "public@shop.test", "", "")
	addr, _ := store.NewAddress(st.ID, store.AddressRoleGeneral, "Street", "", "City", "", "00-001", "PL", "")
	svc := storeidentity.NewService(
		&pubIdentityRepo{id: &idty, addr: &addr},
		&pubStoreRepo{st: &st},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	h := storefront.NewStoreIdentityHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/store/identity", nil)
	req = req.WithContext(store.WithStore(req.Context(), &st))
	rr := httptest.NewRecorder()
	h.Get()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if strings.Contains(body, "smtp") || strings.Contains(body, "mail.smtp") {
		t.Fatalf("public body leaked smtp: %s", body)
	}
	var envelope struct {
		Data storeidentity.PublicIdentity `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	view := envelope.Data
	if view.LegalName != "Legal" || view.EmailGeneral != "public@shop.test" {
		t.Fatalf("%#v body=%s", view, rr.Body.String())
	}
	if view.General == nil || view.General.Line1 != "Street" {
		t.Fatalf("general=%#v", view.General)
	}
}
