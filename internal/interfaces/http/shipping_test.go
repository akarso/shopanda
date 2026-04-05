package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/domain/shipping"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

// ── mock shipping provider ──────────────────────────────────────────────

type mockShippingProvider struct {
	method shipping.ShippingMethod
	rate   shipping.ShippingRate
	err    error
}

func (m *mockShippingProvider) Method() shipping.ShippingMethod { return m.method }

func (m *mockShippingProvider) CalculateRate(_ context.Context, _ string, _ string, _ int) (shipping.ShippingRate, error) {
	return m.rate, m.err
}

// ── helpers ─────────────────────────────────────────────────────────────

func shippingSetup(providers ...shipping.Provider) *http.ServeMux {
	h := shophttp.NewShippingRatesHandler(providers...)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/shipping/rates", h.List())
	return mux
}

func parseShippingBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// ── tests ───────────────────────────────────────────────────────────────

func TestShippingRates_Success(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	provider := &mockShippingProvider{
		method: shipping.MethodFlatRate,
		rate: shipping.ShippingRate{
			ProviderRef: "flat_rate:order-1",
			Cost:        cost,
			Label:       "Flat Rate Shipping",
		},
	}
	mux := shippingSetup(provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/shipping/rates?order_id=order-1&currency=USD&item_count=3", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := parseShippingBody(t, rec)
	data := resp["data"].(map[string]interface{})
	rates := data["rates"].([]interface{})
	if len(rates) != 1 {
		t.Fatalf("rates count = %d, want 1", len(rates))
	}

	rate := rates[0].(map[string]interface{})
	if rate["method"] != "flat_rate" {
		t.Errorf("method = %v, want flat_rate", rate["method"])
	}
	if rate["provider_ref"] != "flat_rate:order-1" {
		t.Errorf("provider_ref = %v, want flat_rate:order-1", rate["provider_ref"])
	}
	if rate["cost"].(float64) != 500 {
		t.Errorf("cost = %v, want 500", rate["cost"])
	}
	if rate["currency"] != "USD" {
		t.Errorf("currency = %v, want USD", rate["currency"])
	}
	if rate["label"] != "Flat Rate Shipping" {
		t.Errorf("label = %v, want Flat Rate Shipping", rate["label"])
	}
}

func TestShippingRates_MissingOrderID(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	provider := &mockShippingProvider{
		method: shipping.MethodFlatRate,
		rate:   shipping.ShippingRate{Cost: cost},
	}
	mux := shippingSetup(provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/shipping/rates?currency=USD&item_count=3", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingRates_MissingCurrency(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	provider := &mockShippingProvider{
		method: shipping.MethodFlatRate,
		rate:   shipping.ShippingRate{Cost: cost},
	}
	mux := shippingSetup(provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/shipping/rates?order_id=order-1&item_count=3", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingRates_MissingItemCount(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	provider := &mockShippingProvider{
		method: shipping.MethodFlatRate,
		rate:   shipping.ShippingRate{Cost: cost},
	}
	mux := shippingSetup(provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/shipping/rates?order_id=order-1&currency=USD", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestShippingRates_InvalidItemCount(t *testing.T) {
	cost := shared.MustNewMoney(500, "USD")
	provider := &mockShippingProvider{
		method: shipping.MethodFlatRate,
		rate:   shipping.ShippingRate{Cost: cost},
	}
	mux := shippingSetup(provider)

	tests := []struct {
		name  string
		value string
	}{
		{"non-numeric", "abc"},
		{"zero", "0"},
		{"negative", "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/shipping/rates?order_id=order-1&currency=USD&item_count="+tt.value, nil)
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
			}
		})
	}
}

func TestShippingRates_ProviderError_Skipped(t *testing.T) {
	provider := &mockShippingProvider{
		method: shipping.MethodFlatRate,
		err:    errors.New("unsupported currency"),
	}
	mux := shippingSetup(provider)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/shipping/rates?order_id=order-1&currency=EUR&item_count=3", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	resp := parseShippingBody(t, rec)
	data := resp["data"].(map[string]interface{})
	rates := data["rates"].([]interface{})
	if len(rates) != 0 {
		t.Fatalf("rates count = %d, want 0", len(rates))
	}
}

func TestShippingRates_PanicsWithNoProviders(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic, got nil")
		}
	}()
	shophttp.NewShippingRatesHandler()
}
