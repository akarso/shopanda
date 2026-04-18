package http_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/event"
)

// ── mock refunder ───────────────────────────────────────────────────────

type mockRefunder struct {
	refundFn func(ctx context.Context, providerRef string, amount int64, currency string) (payment.RefundResult, error)
}

func (m *mockRefunder) Refund(ctx context.Context, providerRef string, amount int64, currency string) (payment.RefundResult, error) {
	if m.refundFn != nil {
		return m.refundFn(ctx, providerRef, amount, currency)
	}
	return payment.RefundResult{ProviderRef: "re_mock"}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func refundSetup(repo payment.PaymentRepository, refunder payment.Refunder) *http.ServeMux {
	bus := event.NewBus(webhookTestLogger())
	h := shophttp.NewRefundHandler(repo, refunder, bus)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/admin/orders/{orderId}/refund", h.Refund())
	return mux
}

func seedCompletedPayment(t *testing.T) *payment.Payment {
	t.Helper()
	amt := shared.MustNewMoney(5000, "EUR")
	p, err := payment.NewPayment("pay-ref-1", "ord-1", payment.MethodStripe, amt)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Complete("pi_existing_123"); err != nil {
		t.Fatal(err)
	}
	return &p
}

// ── success ─────────────────────────────────────────────────────────────

func TestRefund_Success(t *testing.T) {
	py := seedCompletedPayment(t)
	repo := &mockPaymentRepo{
		findByOrderFn: func(_ context.Context, orderID string) (*payment.Payment, error) {
			if orderID == "ord-1" {
				return py, nil
			}
			return nil, nil
		},
	}
	refunder := &mockRefunder{}
	mux := refundSetup(repo, refunder)

	body := `{"amount": 5000}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if py.Status() != payment.StatusRefunded {
		t.Errorf("payment status = %v, want refunded", py.Status())
	}

	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	refund := data["refund"].(map[string]interface{})
	if refund["provider_ref"] != "re_mock" {
		t.Errorf("provider_ref = %v, want re_mock", refund["provider_ref"])
	}
}

// ── validation errors ───────────────────────────────────────────────────

func TestRefund_InvalidBody(t *testing.T) {
	mux := refundSetup(&mockPaymentRepo{}, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader("not json"))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestRefund_ZeroAmount(t *testing.T) {
	mux := refundSetup(&mockPaymentRepo{}, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 0}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestRefund_PaymentNotFound(t *testing.T) {
	repo := &mockPaymentRepo{} // FindByOrderID returns nil, nil
	mux := refundSetup(repo, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 1000}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRefund_PartialRefundRejected(t *testing.T) {
	py := seedCompletedPayment(t) // amount = 5000
	repo := &mockPaymentRepo{
		findByOrderFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	mux := refundSetup(repo, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 2000}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestRefund_AmountExceedsPayment(t *testing.T) {
	py := seedCompletedPayment(t) // amount = 5000
	repo := &mockPaymentRepo{
		findByOrderFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	mux := refundSetup(repo, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 9999}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestRefund_NoProviderRef(t *testing.T) {
	// Pending payment has no ProviderRef set.
	amt := shared.MustNewMoney(5000, "EUR")
	py, _ := payment.NewPayment("pay-1", "ord-1", payment.MethodStripe, amt)
	repo := &mockPaymentRepo{
		findByOrderFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return &py, nil
		},
	}
	mux := refundSetup(repo, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 1000}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestRefund_RefunderError(t *testing.T) {
	py := seedCompletedPayment(t)
	repo := &mockPaymentRepo{
		findByOrderFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	refunder := &mockRefunder{
		refundFn: func(_ context.Context, _ string, _ int64, _ string) (payment.RefundResult, error) {
			return payment.RefundResult{}, errors.New("stripe: charge already refunded")
		},
	}
	mux := refundSetup(repo, refunder)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 5000}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestRefund_NotCompleted(t *testing.T) {
	// Payment is already refunded — status check catches it before provider call.
	amt := shared.MustNewMoney(5000, "EUR")
	py, _ := payment.NewPayment("pay-1", "ord-1", payment.MethodStripe, amt)
	_ = py.Complete("pi_123")
	_ = py.Refund() // now refunded — can't refund again
	repo := &mockPaymentRepo{
		findByOrderFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return &py, nil
		},
	}
	mux := refundSetup(repo, &mockRefunder{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/orders/ord-1/refund", strings.NewReader(`{"amount": 5000}`))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}
