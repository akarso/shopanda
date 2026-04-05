package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── mock payment repo ───────────────────────────────────────────────────

type mockPaymentRepo struct {
	findByIDFn     func(ctx context.Context, id string) (*payment.Payment, error)
	findByOrderFn  func(ctx context.Context, orderID string) (*payment.Payment, error)
	createFn       func(ctx context.Context, p *payment.Payment) error
	updateStatusFn func(ctx context.Context, p *payment.Payment, prev time.Time) error
}

func (m *mockPaymentRepo) FindByID(ctx context.Context, id string) (*payment.Payment, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockPaymentRepo) FindByOrderID(ctx context.Context, orderID string) (*payment.Payment, error) {
	if m.findByOrderFn != nil {
		return m.findByOrderFn(ctx, orderID)
	}
	return nil, nil
}

func (m *mockPaymentRepo) Create(ctx context.Context, p *payment.Payment) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return nil
}

func (m *mockPaymentRepo) UpdateStatus(ctx context.Context, p *payment.Payment, prev time.Time) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, p, prev)
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────

func webhookTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func webhookSetup(repo payment.PaymentRepository) *http.ServeMux {
	bus := event.NewBus(webhookTestLogger())
	h := shophttp.NewPaymentWebhookHandler(repo, bus)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/payments/webhook/{provider}", h.Handle())
	return mux
}

func seedPendingPayment(t *testing.T) *payment.Payment {
	t.Helper()
	amt := shared.MustNewMoney(2500, "EUR")
	p, err := payment.NewPayment("pay-1", "ord-1", payment.MethodManual, amt)
	if err != nil {
		t.Fatalf("seedPendingPayment: %v", err)
	}
	return &p
}

func parseWebhookBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// ── tests ───────────────────────────────────────────────────────────────

func TestPaymentWebhook_Success(t *testing.T) {
	py := seedPendingPayment(t)
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, id string) (*payment.Payment, error) {
			if id == "pay-1" {
				return py, nil
			}
			return nil, nil
		},
	}
	mux := webhookSetup(repo)

	body := `{"payment_id":"pay-1","provider_ref":"ref-abc","success":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", data["status"])
	}
	if py.Status() != payment.StatusCompleted {
		t.Errorf("payment status = %v, want completed", py.Status())
	}
	if py.ProviderRef != "ref-abc" {
		t.Errorf("provider_ref = %v, want ref-abc", py.ProviderRef)
	}
}

func TestPaymentWebhook_Failure(t *testing.T) {
	py := seedPendingPayment(t)
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, id string) (*payment.Payment, error) {
			if id == "pay-1" {
				return py, nil
			}
			return nil, nil
		},
	}
	mux := webhookSetup(repo)

	body := `{"payment_id":"pay-1","provider_ref":"","success":false}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if py.Status() != payment.StatusFailed {
		t.Errorf("payment status = %v, want failed", py.Status())
	}
}

func TestPaymentWebhook_NotFound(t *testing.T) {
	repo := &mockPaymentRepo{}
	mux := webhookSetup(repo)

	body := `{"payment_id":"pay-999","provider_ref":"ref","success":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestPaymentWebhook_UnknownProvider(t *testing.T) {
	repo := &mockPaymentRepo{}
	mux := webhookSetup(repo)

	body := `{"payment_id":"pay-1","provider_ref":"ref","success":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPaymentWebhook_InvalidBody(t *testing.T) {
	repo := &mockPaymentRepo{}
	mux := webhookSetup(repo)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader("not json"))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPaymentWebhook_MissingPaymentID(t *testing.T) {
	repo := &mockPaymentRepo{}
	mux := webhookSetup(repo)

	body := `{"provider_ref":"ref","success":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPaymentWebhook_AlreadyCompleted(t *testing.T) {
	py := seedPendingPayment(t)
	_ = py.Complete("ref-old") // transition to completed

	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	mux := webhookSetup(repo)

	body := `{"payment_id":"pay-1","provider_ref":"ref-new","success":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestPaymentWebhook_RepoError(t *testing.T) {
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return nil, errors.New("db down")
		},
	}
	mux := webhookSetup(repo)

	body := `{"payment_id":"pay-1","provider_ref":"ref","success":true}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/manual", strings.NewReader(body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}
