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
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
)

type pubIdentityRepo struct {
	id   *store.Identity
	addr *store.Address
	tax  []store.TaxIdentifier
}

func (m *pubIdentityRepo) GetIdentity(context.Context, string) (*store.Identity, error) {
	return m.id, nil
}
func (m *pubIdentityRepo) SaveIdentity(context.Context, store.Identity) error { return nil }
func (m *pubIdentityRepo) MutateIdentity(_ context.Context, _ string, fn func(*store.Identity) (store.Identity, error)) (store.Identity, error) {
	return fn(m.id)
}
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
func (m *pubIdentityRepo) MutateAddress(_ context.Context, _ string, _ store.AddressRole, fn func(*store.Address) (store.Address, error)) (store.Address, error) {
	return fn(m.addr)
}
func (m *pubIdentityRepo) ListTaxIdentifiers(context.Context, string) ([]store.TaxIdentifier, error) {
	if m.tax == nil {
		return []store.TaxIdentifier{}, nil
	}
	return m.tax, nil
}
func (m *pubIdentityRepo) UpsertTaxIdentifier(context.Context, store.TaxIdentifier) error {
	return nil
}
func (m *pubIdentityRepo) DeleteTaxIdentifier(context.Context, string, string) error {
	return apperror.NotFound("tax identifier not found")
}

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
	nip, _ := store.NewTaxIdentifier(st.ID, "pl_nip", "1234567890")
	ein, _ := store.NewTaxIdentifier(st.ID, "us_ein", "12-3456789")
	svc := storeidentity.NewService(
		&pubIdentityRepo{id: &idty, addr: &addr, tax: []store.TaxIdentifier{nip, ein}},
		&pubStoreRepo{st: &st},
		store.DefaultTaxIdentifierTypeRegistry(),
	)
	h := storefront.NewStoreIdentityHandler(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/store/identity", nil)
	req.Host = "pl.test"
	req = req.WithContext(store.WithStore(req.Context(), &st))
	rr := httptest.NewRecorder()
	h.Get()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("Cache-Control=%q", cc)
	}
	if vary := rr.Header().Get("Vary"); !strings.Contains(vary, "Host") {
		t.Fatalf("Vary=%q", vary)
	}
	body := rr.Body.String()
	if strings.Contains(body, "smtp") || strings.Contains(body, "mail.smtp") || strings.Contains(body, "us_ein") {
		t.Fatalf("public body leaked secrets or non-imprint tax: %s", body)
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
	if view.General == nil || view.General.Line1 != "Street" || view.General.Role != store.AddressRoleGeneral {
		t.Fatalf("general=%#v", view.General)
	}
	if view.Invoice == nil || view.Invoice.Role != store.AddressRoleInvoice || view.Invoice.Line1 != "Street" {
		t.Fatalf("invoice fallback %#v", view.Invoice)
	}
	if len(view.TaxIDs) != 1 || view.TaxIDs[0].Type != "pl_nip" {
		t.Fatalf("tax %#v", view.TaxIDs)
	}
}
