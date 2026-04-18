package http_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/event"
)

const testStripeWebhookSecret = "whsec_test_secret"

func stripeSign(secret, timestamp string, payload []byte) string {
	signed := fmt.Sprintf("%s.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	return hex.EncodeToString(mac.Sum(nil))
}

func stripeSignatureHeader(secret string, payload []byte) string {
	ts := fmt.Sprintf("%d", time.Now().Unix())
	sig := stripeSign(secret, ts, payload)
	return fmt.Sprintf("t=%s,v1=%s", ts, sig)
}

func stripeWebhookSetup(repo payment.PaymentRepository) *http.ServeMux {
	bus := event.NewBus(webhookTestLogger())
	h := shophttp.NewStripeWebhookHandler(repo, bus, testStripeWebhookSecret)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/payments/webhook/stripe", h.Handle())
	return mux
}

func seedStripePayment(t *testing.T) *payment.Payment {
	t.Helper()
	amt := shared.MustNewMoney(5000, "EUR")
	p, err := payment.NewPayment("pay-stripe-1", "ord-1", payment.MethodStripe, amt)
	if err != nil {
		t.Fatalf("seedStripePayment: %v", err)
	}
	return &p
}

func stripeEvent(evtType, piID, paymentID string) []byte {
	evt := map[string]interface{}{
		"id":   "evt_test_001",
		"type": evtType,
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id": piID,
				"metadata": map[string]string{
					"payment_id": paymentID,
				},
			},
		},
	}
	b, _ := json.Marshal(evt)
	return b
}

// ── success tests ───────────────────────────────────────────────────────

func TestStripeWebhook_PaymentIntentSucceeded(t *testing.T) {
	py := seedStripePayment(t)
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, id string) (*payment.Payment, error) {
			if id == "pay-stripe-1" {
				return py, nil
			}
			return nil, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := stripeEvent("payment_intent.succeeded", "pi_test_123", "pay-stripe-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
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
	if py.ProviderRef != "pi_test_123" {
		t.Errorf("provider_ref = %v, want pi_test_123", py.ProviderRef)
	}
}

func TestStripeWebhook_PaymentIntentFailed(t *testing.T) {
	py := seedStripePayment(t)
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, id string) (*payment.Payment, error) {
			if id == "pay-stripe-1" {
				return py, nil
			}
			return nil, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := stripeEvent("payment_intent.payment_failed", "pi_test_123", "pay-stripe-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if py.Status() != payment.StatusFailed {
		t.Errorf("payment status = %v, want failed", py.Status())
	}
}

// ── signature tests ─────────────────────────────────────────────────────

func TestStripeWebhook_MissingSignature(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	body := stripeEvent("payment_intent.succeeded", "pi_1", "pay-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	// No Stripe-Signature header
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestStripeWebhook_InvalidSignature(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	body := stripeEvent("payment_intent.succeeded", "pi_1", "pay-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", "t=12345,v1=0000000000000000000000000000000000000000000000000000000000000000")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// ── idempotency tests ───────────────────────────────────────────────────

func TestStripeWebhook_AlreadyCompleted(t *testing.T) {
	py := seedStripePayment(t)
	_ = py.Complete("pi_prev")

	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := stripeEvent("payment_intent.succeeded", "pi_test_123", "pay-stripe-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "already_processed" {
		t.Errorf("status = %v, want already_processed", data["status"])
	}
}

func TestStripeWebhook_AlreadyFailed(t *testing.T) {
	py := seedStripePayment(t)
	_ = py.Fail()

	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := stripeEvent("payment_intent.payment_failed", "pi_test_123", "pay-stripe-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "already_processed" {
		t.Errorf("status = %v, want already_processed", data["status"])
	}
}

// ── edge case tests ─────────────────────────────────────────────────────

func TestStripeWebhook_UnhandledEventType(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	evt := map[string]interface{}{
		"id":   "evt_test_unhandled",
		"type": "charge.dispute.created",
		"data": map[string]interface{}{"object": map[string]interface{}{}},
	}
	body, _ := json.Marshal(evt)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "ignored" {
		t.Errorf("status = %v, want ignored", data["status"])
	}
}

func TestStripeWebhook_MissingEventID(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	evt := map[string]interface{}{
		"type": "payment_intent.succeeded",
		"data": map[string]interface{}{"object": map[string]interface{}{}},
	}
	body, _ := json.Marshal(evt)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestStripeWebhook_MissingPaymentIDInMetadata(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	evt := map[string]interface{}{
		"id":   "evt_test_no_pid",
		"type": "payment_intent.succeeded",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id":       "pi_123",
				"metadata": map[string]string{},
			},
		},
	}
	body, _ := json.Marshal(evt)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestStripeWebhook_PaymentNotFound(t *testing.T) {
	repo := &mockPaymentRepo{} // FindByID returns nil, nil
	mux := stripeWebhookSetup(repo)

	body := stripeEvent("payment_intent.succeeded", "pi_123", "pay-unknown")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestStripeWebhook_ProviderMismatch(t *testing.T) {
	// Create a manual payment, not stripe.
	amt := shared.MustNewMoney(1000, "EUR")
	py, err := payment.NewPayment("pay-manual-1", "ord-1", payment.MethodManual, amt)
	if err != nil {
		t.Fatal(err)
	}
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return &py, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := stripeEvent("payment_intent.succeeded", "pi_123", "pay-manual-1")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestStripeWebhook_InvalidJSON(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	body := []byte("not json")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

// ── charge.refunded tests ───────────────────────────────────────────────

func chargeRefundedEvent(chargeID, piID, paymentID string, amount, amountRefunded int64) []byte {
	evt := map[string]interface{}{
		"id":   "evt_refund_001",
		"type": "charge.refunded",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id":              chargeID,
				"payment_intent":  piID,
				"amount":          amount,
				"amount_refunded": amountRefunded,
				"metadata": map[string]string{
					"payment_id": paymentID,
				},
			},
		},
	}
	b, _ := json.Marshal(evt)
	return b
}

func seedCompletedStripePayment(t *testing.T) *payment.Payment {
	t.Helper()
	amt := shared.MustNewMoney(5000, "EUR")
	p, err := payment.NewPayment("pay-stripe-ref-1", "ord-1", payment.MethodStripe, amt)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Complete("pi_completed_123"); err != nil {
		t.Fatal(err)
	}
	return &p
}

func TestStripeWebhook_ChargeRefunded(t *testing.T) {
	py := seedCompletedStripePayment(t)
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, id string) (*payment.Payment, error) {
			if id == "pay-stripe-ref-1" {
				return py, nil
			}
			return nil, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := chargeRefundedEvent("ch_123", "pi_completed_123", "pay-stripe-ref-1", 5000, 5000)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "accepted" {
		t.Errorf("status = %v, want accepted", data["status"])
	}
	if py.Status() != payment.StatusRefunded {
		t.Errorf("payment status = %v, want refunded", py.Status())
	}
}

func TestStripeWebhook_ChargeRefunded_AlreadyRefunded(t *testing.T) {
	py := seedCompletedStripePayment(t)
	_ = py.Refund() // already refunded

	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return py, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := chargeRefundedEvent("ch_123", "pi_completed_123", "pay-stripe-ref-1", 5000, 5000)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	resp := parseWebhookBody(t, rec)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "already_processed" {
		t.Errorf("status = %v, want already_processed", data["status"])
	}
}

func TestStripeWebhook_ChargeRefunded_MissingMetadata(t *testing.T) {
	mux := stripeWebhookSetup(&mockPaymentRepo{})

	evt := map[string]interface{}{
		"id":   "evt_refund_no_meta",
		"type": "charge.refunded",
		"data": map[string]interface{}{
			"object": map[string]interface{}{
				"id":             "ch_123",
				"payment_intent": "pi_123",
				"metadata":       map[string]string{},
			},
		},
	}
	body, _ := json.Marshal(evt)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestStripeWebhook_ChargeRefunded_ProviderMismatch(t *testing.T) {
	amt := shared.MustNewMoney(1000, "EUR")
	py, _ := payment.NewPayment("pay-manual-ref", "ord-1", payment.MethodManual, amt)
	repo := &mockPaymentRepo{
		findByIDFn: func(_ context.Context, _ string) (*payment.Payment, error) {
			return &py, nil
		},
	}
	mux := stripeWebhookSetup(repo)

	body := chargeRefundedEvent("ch_123", "pi_123", "pay-manual-ref", 1000, 1000)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestStripeWebhook_ChargeRefunded_PaymentNotFound(t *testing.T) {
	repo := &mockPaymentRepo{} // FindByID returns nil, nil
	mux := stripeWebhookSetup(repo)

	body := chargeRefundedEvent("ch_123", "pi_123", "pay-unknown", 5000, 5000)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/payments/webhook/stripe", strings.NewReader(string(body)))
	req.Header.Set("Stripe-Signature", stripeSignatureHeader(testStripeWebhookSecret, body))
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
