package storefront_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	storecreditApp "github.com/akarso/shopanda/internal/application/storecredit"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/shared"
	credit "github.com/akarso/shopanda/internal/domain/storecredit"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
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

func (s *stubStoreCreditRepo) Issue(_ context.Context, _ string, amount shared.Money, _, _ string) error {
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

// noopStoreCreditCustomerRepo is a no-op customer.CustomerRepository stub —
// storecreditApp.NewService requires one, but account-balance lookups here
// don't exercise it.
type noopStoreCreditCustomerRepo struct{}

func (noopStoreCreditCustomerRepo) FindByID(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}

func (noopStoreCreditCustomerRepo) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}

func (noopStoreCreditCustomerRepo) Create(_ context.Context, _ *customer.Customer) error {
	return nil
}

func (noopStoreCreditCustomerRepo) Update(_ context.Context, _ *customer.Customer) error {
	return nil
}

func (noopStoreCreditCustomerRepo) ListCustomers(_ context.Context, _, _ int) ([]customer.Customer, error) {
	return nil, nil
}

func (noopStoreCreditCustomerRepo) BumpTokenGeneration(_ context.Context, _ string) error {
	return nil
}

func (noopStoreCreditCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _, _ string) error {
	return nil
}

func (noopStoreCreditCustomerRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func TestStoreCreditAccountHandler_GetBalance(t *testing.T) {
	repo := &stubStoreCreditRepo{balance: 1200}
	svc := storecreditApp.NewService(repo, noopStoreCreditCustomerRepo{})
	h := storefront.NewStoreCreditAccountHandler(svc)

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
	svc := storecreditApp.NewService(repo, noopStoreCreditCustomerRepo{})
	h := storefront.NewStoreCreditAccountHandler(svc)

	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/account/store-credit", h.GetBalance())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/store-credit?currency=EUR", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
