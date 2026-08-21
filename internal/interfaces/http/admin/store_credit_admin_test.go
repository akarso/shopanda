package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

type stubStoreCreditRepo struct {
	balance int64
	entries []credit.Entry
	issued  []shared.Money
}

func (s *stubStoreCreditRepo) GetBalance(_ context.Context, _, currency string) (shared.Money, error) {
	return shared.MustNewMoney(s.balance, currency), nil
}

func (s *stubStoreCreditRepo) Issue(_ context.Context, _ string, amount shared.Money, _ string) error {
	s.issued = append(s.issued, amount)
	s.balance += amount.Amount()
	return nil
}

func (s *stubStoreCreditRepo) Redeem(_ context.Context, _, _ string, amount shared.Money) error {
	return nil
}

func (s *stubStoreCreditRepo) ListLedger(_ context.Context, _, _ string, _, _ int) ([]credit.Entry, error) {
	return s.entries, nil
}

func newStoreCreditAdminRouter(h *admin.StoreCreditAdminHandler) *http.ServeMux {
	requireCustomersRead := shophttp.RequirePermission(rbac.CustomersRead)
	requireCustomersWrite := shophttp.RequirePermission(rbac.CustomersWrite)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/customers/{customerId}/store-credit", withAdminContext(requireCustomersRead(h.Get())))
	mux.Handle("POST /api/v1/admin/customers/{customerId}/store-credit/issue", withAdminContext(requireCustomersWrite(h.Issue())))
	return mux
}

func TestStoreCreditAdminHandler_Issue(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	customers := &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, id string) (*customer.Customer, error) {
			if id != "cust-1" {
				return nil, nil
			}
			c, err := customer.NewCustomer("cust-1", "cust@example.com")
			if err != nil {
				return nil, err
			}
			return &c, nil
		},
	}
	svc := storecreditApp.NewService(repo, customers)
	h := admin.NewStoreCreditAdminHandler(svc)
	mux := newStoreCreditAdminRouter(h)

	body, _ := json.Marshal(map[string]interface{}{
		"amount": 2500,
		"note":   "goodwill",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/customers/cust-1/store-credit/issue", bytes.NewReader(body))
	req.SetPathValue("customerId", "cust-1")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Store-ID", "store-1")
	req.Header.Set("X-Admin-Currency", "EUR")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	balance := resp.Data["balance"].(map[string]interface{})
	if balance["amount"].(float64) != 2500 {
		t.Errorf("balance amount = %v, want 2500", balance["amount"])
	}
	if len(repo.issued) != 1 || repo.issued[0].Amount() != 2500 {
		t.Errorf("issued = %+v, want 2500", repo.issued)
	}
}

func TestStoreCreditAdminHandler_Get(t *testing.T) {
	repo := &stubStoreCreditRepo{
		balance: 1800,
		entries: []credit.Entry{
			{
				ID:         "entry-1",
				CustomerID: "cust-1",
				Currency:   "EUR",
				Amount:     shared.MustNewMoney(500, "EUR"),
				Kind:       credit.KindIssue,
				Note:       "goodwill",
			},
		},
	}
	svc := storecreditApp.NewService(repo, &mockAdminCustomerRepo{})
	h := admin.NewStoreCreditAdminHandler(svc)
	mux := newStoreCreditAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/customers/cust-1/store-credit?offset=0&limit=10", nil)
	req.SetPathValue("customerId", "cust-1")
	req.Header.Set("X-Admin-Store-ID", "store-1")
	req.Header.Set("X-Admin-Currency", "EUR")
	req = testhelper.AdminRequest(req, "admin-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	balance := resp.Data["balance"].(map[string]interface{})
	if balance["amount"].(float64) != 1800 {
		t.Errorf("balance amount = %v, want 1800", balance["amount"])
	}
	if balance["currency"] != "EUR" {
		t.Errorf("balance currency = %v, want EUR", balance["currency"])
	}
	ledger, ok := resp.Data["ledger"].([]interface{})
	if !ok || len(ledger) != 1 {
		t.Fatalf("ledger = %#v, want one entry", resp.Data["ledger"])
	}
	entry := ledger[0].(map[string]interface{})
	if entry["id"] != "entry-1" {
		t.Errorf("ledger id = %v, want entry-1", entry["id"])
	}
	if entry["kind"] != "issue" {
		t.Errorf("ledger kind = %v, want issue", entry["kind"])
	}
	if entry["amount"].(float64) != 500 {
		t.Errorf("ledger amount = %v, want 500", entry["amount"])
	}
	if entry["currency"] != "EUR" {
		t.Errorf("ledger currency = %v, want EUR", entry["currency"])
	}
	if entry["note"] != "goodwill" {
		t.Errorf("ledger note = %v, want goodwill", entry["note"])
	}
}
