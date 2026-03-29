package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

func TestHealthHandler(t *testing.T) {
	handler := shophttp.HealthHandler()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var resp shophttp.HealthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %s", resp.Status)
	}
}
