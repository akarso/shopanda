package http_test

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
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
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

func newStoreCreditAdminRouter(h *shophttp.StoreCreditAdminHandler) *http.ServeMux {
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
	h := shophttp.NewStoreCreditAdminHandler(svc)
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

func TestStoreCreditAccountHandler_GetBalance(t *testing.T) {
	repo := &stubStoreCreditRepo{balance: 1200}
	svc := storecreditApp.NewService(repo, &mockAdminCustomerRepo{})
	h := shophttp.NewStoreCreditAccountHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/account/store-credit", h.GetBalance())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/store-credit?currency=EUR", nil)
	req = testhelper.CustomerRequest(req, "cust-1")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestStoreCreditAccountHandler_GetBalance_Unauthorized(t *testing.T) {
	repo := &stubStoreCreditRepo{}
	svc := storecreditApp.NewService(repo, &mockAdminCustomerRepo{})
	h := shophttp.NewStoreCreditAccountHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/account/store-credit", h.GetBalance())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/store-credit?currency=EUR", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
