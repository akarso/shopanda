package stripepay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/payment"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/infrastructure/stripepay"
)

func testPayment(t *testing.T) *payment.Payment {
	t.Helper()
	py, err := payment.NewPayment("pay-1", "ord-1", payment.MethodStripe, shared.MustNewMoney(2999, "USD"))
	if err != nil {
		t.Fatalf("NewPayment: %v", err)
	}
	return &py
}

func TestProvider_Method(t *testing.T) {
	p, err := stripepay.NewProvider("sk_test_123")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.Method() != payment.MethodStripe {
		t.Errorf("Method() = %q, want stripe", p.Method())
	}
}

func TestProvider_Initiate_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/payment_intents" {
			t.Errorf("path = %s, want /v1/payment_intents", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk_test_123" {
			t.Errorf("Authorization = %q, want Bearer sk_test_123", r.Header.Get("Authorization"))
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", ct)
		}
		if ik := r.Header.Get("Idempotency-Key"); ik != "pay-1" {
			t.Errorf("Idempotency-Key = %q, want pay-1", ik)
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if r.FormValue("amount") != "2999" {
			t.Errorf("amount = %q, want 2999", r.FormValue("amount"))
		}
		if r.FormValue("currency") != "usd" {
			t.Errorf("currency = %q, want usd", r.FormValue("currency"))
		}
		if r.FormValue("metadata[order_id]") != "ord-1" {
			t.Errorf("metadata[order_id] = %q, want ord-1", r.FormValue("metadata[order_id]"))
		}
		if r.FormValue("metadata[payment_id]") != "pay-1" {
			t.Errorf("metadata[payment_id] = %q, want pay-1", r.FormValue("metadata[payment_id]"))
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":            "pi_test_abc123",
			"client_secret": "pi_test_abc123_secret_xyz",
			"status":        "requires_payment_method",
		})
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	py := testPayment(t)

	result, err := p.Initiate(context.Background(), py)
	if err != nil {
		t.Fatalf("Initiate: %v", err)
	}

	if result.ProviderRef != "pi_test_abc123" {
		t.Errorf("ProviderRef = %q, want pi_test_abc123", result.ProviderRef)
	}
	if !result.Pending {
		t.Error("expected Pending = true")
	}
	if result.ClientSecret != "pi_test_abc123_secret_xyz" {
		t.Errorf("ClientSecret = %q, want pi_test_abc123_secret_xyz", result.ClientSecret)
	}
	if result.Success {
		t.Error("expected Success = false for Stripe (pending)")
	}
}

func TestProvider_Initiate_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "invalid_request_error",
				"message": "Amount must be at least 50 cents",
			},
		})
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	py := testPayment(t)

	_, err = p.Initiate(context.Background(), py)
	if err == nil {
		t.Fatal("expected error from API")
	}
	if got := err.Error(); got != "stripepay: API error 400: Amount must be at least 50 cents" {
		t.Errorf("error = %q", got)
	}
}

func TestProvider_Initiate_APIErrorNoMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal"))
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	py := testPayment(t)

	_, err = p.Initiate(context.Background(), py)
	if err == nil {
		t.Fatal("expected error from API")
	}
	if got := err.Error(); got != "stripepay: API error 500" {
		t.Errorf("error = %q", got)
	}
}

func TestProvider_Initiate_NilPayment(t *testing.T) {
	p, err := stripepay.NewProvider("sk_test_123")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_, err = p.Initiate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil payment")
	}
}

func TestProvider_Initiate_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close immediately to guarantee connection refused

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	py := testPayment(t)

	_, err = p.Initiate(context.Background(), py)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestProvider_Initiate_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{invalid"))
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	py := testPayment(t)

	_, err = p.Initiate(context.Background(), py)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewProvider_EmptyKey(t *testing.T) {
	_, err := stripepay.NewProvider("")
	if err == nil {
		t.Fatal("expected error for empty secret key")
	}
}

func TestProvider_Initiate_MissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "pi_test_abc123",
			"status": "requires_payment_method",
			// client_secret intentionally missing
		})
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, err = p.Initiate(context.Background(), testPayment(t))
	if err == nil {
		t.Fatal("expected error for missing client_secret")
	}
}

// ── Refund tests ────────────────────────────────────────────────────────

func TestProvider_Refund_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/refunds" {
			t.Errorf("path = %s, want /v1/refunds", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.FormValue("payment_intent"); got != "pi_abc123" {
			t.Errorf("payment_intent = %q, want pi_abc123", got)
		}
		if got := r.FormValue("amount"); got != "1500" {
			t.Errorf("amount = %q, want 1500", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "refund:pi_abc123:1500" {
			t.Errorf("Idempotency-Key = %q, want refund:pi_abc123:1500", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":     "re_test_001",
			"status": "succeeded",
		})
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	result, err := p.Refund(context.Background(), "pi_abc123", 1500, "usd")
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if result.ProviderRef != "re_test_001" {
		t.Errorf("ProviderRef = %q, want re_test_001", result.ProviderRef)
	}
}

func TestProvider_Refund_EmptyProviderRef(t *testing.T) {
	p, err := stripepay.NewProvider("sk_test_123")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	_, err = p.Refund(context.Background(), "", 1500, "usd")
	if err == nil {
		t.Fatal("expected error for empty provider ref")
	}
}

func TestProvider_Refund_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]interface{}{
				"type":    "invalid_request_error",
				"message": "charge already refunded",
			},
		})
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, err = p.Refund(context.Background(), "pi_abc123", 1500, "usd")
	if err == nil {
		t.Fatal("expected error for API error")
	}
}

func TestProvider_Refund_MissingID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "succeeded",
		})
	}))
	defer srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, err = p.Refund(context.Background(), "pi_abc123", 1500, "usd")
	if err == nil {
		t.Fatal("expected error for missing id in refund response")
	}
}

func TestProvider_Refund_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	p, err := stripepay.NewProvider("sk_test_123", stripepay.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	_, err = p.Refund(context.Background(), "pi_abc123", 1500, "usd")
	if err == nil {
		t.Fatal("expected error for network failure")
	}
}
