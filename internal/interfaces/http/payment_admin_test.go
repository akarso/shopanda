package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type mockPaymentListRepo struct {
	list []payment.Payment
	last payment.ListFilter
}

func (m *mockPaymentListRepo) FindByID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockPaymentListRepo) FindByOrderID(context.Context, string) (*payment.Payment, error) {
	return nil, nil
}
func (m *mockPaymentListRepo) Create(context.Context, *payment.Payment) error { return nil }
func (m *mockPaymentListRepo) UpdateStatus(context.Context, *payment.Payment, time.Time) error {
	return nil
}
func (m *mockPaymentListRepo) List(_ context.Context, filter payment.ListFilter) ([]payment.Payment, error) {
	m.last = filter
	return m.list, nil
}

func TestPaymentAdmin_List_ReturnsPayments(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	pay, err := payment.NewPaymentFromDB("pay-1", "ord-1", payment.MethodStripe, string(payment.StatusCompleted),
		shared.MustNewMoney(2500, "EUR"), "pi_123", now, now)
	if err != nil {
		t.Fatalf("NewPaymentFromDB: %v", err)
	}

	repo := &mockPaymentListRepo{list: []payment.Payment{*pay}}
	h := shophttp.NewPaymentAdminHandler(repo, admin.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/payments", shophttp.RequirePermission(rbac.OrdersRead)(h.List()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/payments?offset=0&limit=20&status=completed", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if repo.last.Status != payment.StatusCompleted {
		t.Fatalf("filter status = %q", repo.last.Status)
	}

	var envelope struct {
		Data struct {
			Payments []map[string]interface{} `json:"payments"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Payments) != 1 {
		t.Fatalf("payments len = %d, want 1", len(envelope.Data.Payments))
	}
	if envelope.Data.Payments[0]["order_id"] != "ord-1" {
		t.Fatalf("order_id = %v", envelope.Data.Payments[0]["order_id"])
	}
}

func TestPaymentAdmin_List_InvalidStatus(t *testing.T) {
	repo := &mockPaymentListRepo{}
	h := shophttp.NewPaymentAdminHandler(repo, admin.NewAuditor(logger.New("error")))
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/payments", shophttp.RequirePermission(rbac.OrdersRead)(h.List()))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/payments?status=bogus", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}
