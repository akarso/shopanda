package http_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/legal"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── stubs ───────────────────────────────────────────────────────────────

type stubCustomerRepo struct {
	customers map[string]*customer.Customer
	deleted   map[string]bool
}

func newStubCustomerRepo() *stubCustomerRepo {
	return &stubCustomerRepo{
		customers: make(map[string]*customer.Customer),
		deleted:   make(map[string]bool),
	}
}

func (r *stubCustomerRepo) FindByID(_ context.Context, id string) (*customer.Customer, error) {
	c, ok := r.customers[id]
	if !ok {
		return nil, nil
	}
	return c, nil
}
func (r *stubCustomerRepo) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}
func (r *stubCustomerRepo) Create(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	return nil
}
func (r *stubCustomerRepo) Update(_ context.Context, c *customer.Customer) error {
	r.customers[c.ID] = c
	return nil
}
func (r *stubCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}
func (r *stubCustomerRepo) BumpTokenGeneration(_ context.Context, _ string) error { return nil }
func (r *stubCustomerRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.customers[id]; !ok {
		return nil // simplified; real impl returns NotFound
	}
	delete(r.customers, id)
	r.deleted[id] = true
	return nil
}

type stubConsentRepo struct {
	consents map[string]*legal.Consent
}

func newStubConsentRepo() *stubConsentRepo {
	return &stubConsentRepo{consents: make(map[string]*legal.Consent)}
}

func (r *stubConsentRepo) FindByCustomerID(_ context.Context, id string) (*legal.Consent, error) {
	c, ok := r.consents[id]
	if !ok {
		return nil, nil
	}
	return c, nil
}
func (r *stubConsentRepo) Upsert(_ context.Context, c *legal.Consent) error {
	r.consents[c.CustomerID] = c
	return nil
}
func (r *stubConsentRepo) DeleteByCustomerID(_ context.Context, id string) error {
	delete(r.consents, id)
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func accountBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
}

func accountSetup() (*stubCustomerRepo, *stubOrderRepo, *stubConsentRepo, *http.ServeMux) {
	cr := newStubCustomerRepo()
	or := newStubOrderRepo()
	lr := newStubConsentRepo()
	bus := accountBus()

	h := shophttp.NewAccountHandler(cr, or, lr, bus)

	requireAuth := shophttp.RequireAuth()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/account/consent", requireAuth(h.GetConsent()))
	mux.Handle("PUT /api/v1/account/consent", requireAuth(h.UpdateConsent()))
	mux.Handle("GET /api/v1/account/data", requireAuth(h.GetData()))
	mux.Handle("GET /api/v1/account/export", requireAuth(h.Export()))
	mux.Handle("DELETE /api/v1/account", requireAuth(h.Delete()))
	return cr, or, lr, mux
}

func seedCustomer(t *testing.T, repo *stubCustomerRepo, id string) {
	t.Helper()
	c, err := customer.NewCustomer(id, id+"@example.com")
	if err != nil {
		t.Fatalf("seedCustomer: %v", err)
	}
	c.FirstName = "Jane"
	c.LastName = "Doe"
	repo.customers[id] = &c
}

func parseJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return body
}

// ── GET /api/v1/account/consent ─────────────────────────────────────────

func TestAccountHandler_GetConsent_Default(t *testing.T) {
	_, _, _, mux := accountSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/account/consent", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseJSON(t, rec)
	data := body["data"].(map[string]interface{})
	consent := data["consent"].(map[string]interface{})

	if consent["necessary"] != true {
		t.Errorf("necessary = %v, want true", consent["necessary"])
	}
	if consent["analytics"] != false {
		t.Errorf("analytics = %v, want false", consent["analytics"])
	}
	if consent["marketing"] != false {
		t.Errorf("marketing = %v, want false", consent["marketing"])
	}
}

func TestAccountHandler_GetConsent_Existing(t *testing.T) {
	_, _, lr, mux := accountSetup()

	c, _ := legal.NewConsent("cust-1")
	c.Update(true, false)
	lr.consents["cust-1"] = &c

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/account/consent", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseJSON(t, rec)
	data := body["data"].(map[string]interface{})
	consent := data["consent"].(map[string]interface{})

	if consent["analytics"] != true {
		t.Errorf("analytics = %v, want true", consent["analytics"])
	}
	if consent["marketing"] != false {
		t.Errorf("marketing = %v, want false", consent["marketing"])
	}
}

func TestAccountHandler_GetConsent_Unauthenticated(t *testing.T) {
	_, _, _, mux := accountSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/account/consent", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// ── PUT /api/v1/account/consent ─────────────────────────────────────────

func TestAccountHandler_UpdateConsent_OK(t *testing.T) {
	_, _, lr, mux := accountSetup()

	rec := httptest.NewRecorder()
	body := `{"analytics":true,"marketing":true}`
	req := httptest.NewRequest("PUT", "/api/v1/account/consent", strings.NewReader(body))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := parseJSON(t, rec)
	data := resp["data"].(map[string]interface{})
	consent := data["consent"].(map[string]interface{})

	if consent["necessary"] != true {
		t.Errorf("necessary = %v, want true", consent["necessary"])
	}
	if consent["analytics"] != true {
		t.Errorf("analytics = %v, want true", consent["analytics"])
	}
	if consent["marketing"] != true {
		t.Errorf("marketing = %v, want true", consent["marketing"])
	}

	// Verify persisted.
	if lr.consents["cust-1"] == nil {
		t.Fatal("consent not persisted")
	}
}

func TestAccountHandler_UpdateConsent_InvalidBody(t *testing.T) {
	_, _, _, mux := accountSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/account/consent", strings.NewReader("not json"))
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

// ── GET /api/v1/account/data ────────────────────────────────────────────

func TestAccountHandler_GetData_OK(t *testing.T) {
	cr, or, lr, mux := accountSetup()
	seedCustomer(t, cr, "cust-1")
	seedOrder(t, or, "ord-1", "cust-1")

	c, _ := legal.NewConsent("cust-1")
	lr.consents["cust-1"] = &c

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/account/data", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseJSON(t, rec)
	data := body["data"].(map[string]interface{})
	acct := data["account"].(map[string]interface{})

	cust := acct["customer"].(map[string]interface{})
	if cust["id"] != "cust-1" {
		t.Errorf("customer.id = %v, want cust-1", cust["id"])
	}
	if cust["email"] != "cust-1@example.com" {
		t.Errorf("customer.email = %v", cust["email"])
	}

	orders := acct["orders"].([]interface{})
	if len(orders) != 1 {
		t.Fatalf("orders len = %d, want 1", len(orders))
	}
}

func TestAccountHandler_GetData_NotFound(t *testing.T) {
	_, _, _, mux := accountSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/account/data", nil)
	req = testhelper.CustomerRequest(req, "cust-999")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// ── GET /api/v1/account/export ──────────────────────────────────────────

func TestAccountHandler_Export_ContentDisposition(t *testing.T) {
	cr, _, _, mux := accountSetup()
	seedCustomer(t, cr, "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/account/export", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	cd := rec.Header().Get("Content-Disposition")
	if cd == "" {
		t.Fatal("Content-Disposition header missing")
	}
	if !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
}

// ── DELETE /api/v1/account ──────────────────────────────────────────────

func TestAccountHandler_Delete_OK(t *testing.T) {
	cr, _, lr, mux := accountSetup()
	seedCustomer(t, cr, "cust-1")
	c, _ := legal.NewConsent("cust-1")
	lr.consents["cust-1"] = &c

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/account", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Customer removed.
	if _, ok := cr.customers["cust-1"]; ok {
		t.Error("customer should have been deleted")
	}
	if !cr.deleted["cust-1"] {
		t.Error("deleted flag should be set")
	}

	// Consent removed.
	if _, ok := lr.consents["cust-1"]; ok {
		t.Error("consent should have been deleted")
	}
}

func TestAccountHandler_Delete_Unauthenticated(t *testing.T) {
	_, _, _, mux := accountSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/account", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
