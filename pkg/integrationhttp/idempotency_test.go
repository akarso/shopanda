package integrationhttp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

func TestIdempotencyHandler_FirstRequestRunsHandler(t *testing.T) {
	store := integrationhttp.NewMemoryIdempotencyStore()
	calls := 0
	handler := integrationhttp.IdempotencyHandler(integrationhttp.IdempotencyConfig{
		Store:      store,
		PluginSlug: "acme",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		integrationhttp.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}))

	body := []byte(`{"order_id":"100"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
	req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "key-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted || calls != 1 {
		t.Fatalf("status = %d calls = %d body = %s", rec.Code, calls, rec.Body.String())
	}
}

func TestIdempotencyHandler_ReplayReturnsStoredResponse(t *testing.T) {
	store := integrationhttp.NewMemoryIdempotencyStore()
	calls := 0
	handler := integrationhttp.IdempotencyHandler(integrationhttp.IdempotencyConfig{
		Store:      store,
		PluginSlug: "acme",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		integrationhttp.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	}))

	body := []byte(`{"order_id":"100"}`)
	makeReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
		req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "key-replay")
		return req
	}

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, makeReq())
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, makeReq())
	if rec1.Code != http.StatusAccepted || rec2.Code != http.StatusAccepted {
		t.Fatalf("statuses = %d %d", rec1.Code, rec2.Code)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	if rec2.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatalf("missing replay header: %+v", rec2.Header())
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("bodies differ: %q vs %q", rec1.Body.String(), rec2.Body.String())
	}
}

func TestIdempotencyHandler_ConflictOnDifferentBody(t *testing.T) {
	store := integrationhttp.NewMemoryIdempotencyStore()
	handler := integrationhttp.IdempotencyHandler(integrationhttp.IdempotencyConfig{
		Store:      store,
		PluginSlug: "acme",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integrationhttp.WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	req1 := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{"a":1}`)))
	req1.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "key-conflict")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{"a":2}`)))
	req2.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "key-conflict")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict || !strings.Contains(rec2.Body.String(), extapi.IntegrationErrorIdempotencyConflict) {
		t.Fatalf("status = %d body = %s", rec2.Code, rec2.Body.String())
	}
}

func TestIdempotencyHandler_SkipsWithoutKey(t *testing.T) {
	store := integrationhttp.NewMemoryIdempotencyStore()
	calls := 0
	handler := integrationhttp.IdempotencyHandler(integrationhttp.IdempotencyConfig{
		Store:      store,
		PluginSlug: "acme",
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/x", nil))
	if rec.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls)
	}
}

func TestIdempotencyHandler_NilStorePassthrough(t *testing.T) {
	calls := 0
	handler := integrationhttp.IdempotencyHandler(integrationhttp.IdempotencyConfig{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "ignored")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status = %d calls = %d", rec.Code, calls)
	}
}
