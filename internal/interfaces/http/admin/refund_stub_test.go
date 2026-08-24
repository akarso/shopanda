package admin_test

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"encoding/json"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// mockPaymentRepo duplicates the storefront payment_webhook_test.go mock —
// unexported, so it can't be shared across the http_test/admin_test package
// boundary created by the admin package split.
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

func (m *mockPaymentRepo) List(context.Context, payment.ListFilter) ([]payment.Payment, error) {
	return nil, nil
}

func refundTestLogger() logger.Logger {
	return logger.NewWithWriter(io.Discard, "error")
}

func parseRefundBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}
