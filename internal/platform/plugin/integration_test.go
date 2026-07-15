package plugin_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/pkg/extapi"
	"github.com/akarso/shopanda/pkg/integrationhttp"
)

func TestApp_Integration_RegisterRoute(t *testing.T) {
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integrationhttp.WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
	if err := app.Integration("acme").RegisterRoute("POST", "order-status", handler); err != nil {
		t.Fatalf("RegisterRoute: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 1 || routes[0].Pattern != "POST /api/v1/integrations/acme/order-status" {
		t.Fatalf("routes = %+v", routes)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", nil)
	routes[0].Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestApp_Integration_RegisterAdminRoute(t *testing.T) {
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integrationhttp.WriteError(w, http.StatusForbidden, "forbidden", "nope", nil)
	})
	if err := app.Integration("acme").RegisterAdminRoute("GET", "health", rbac.Permission("integrations.read"), handler); err != nil {
		t.Fatalf("RegisterAdminRoute: %v", err)
	}
	routes := app.AdminRoutes()
	if len(routes) != 1 || routes[0].Pattern != "GET /api/v1/admin/integrations/acme/health" {
		t.Fatalf("routes = %+v", routes)
	}
}

func TestApp_Integration_RegisterSecureRoute(t *testing.T) {
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	replay := integrationhttp.NewMemoryReplayStore()
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"ok":true}`)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		integrationhttp.WriteJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
	auth := integrationhttp.AuthConfig{
		APIKey:      "secret",
		HMACSecret:  "hmac",
		ReplayStore: replay,
		Now:         func() time.Time { return now },
	}
	if err := app.Integration("acme").RegisterSecureRoute("POST", "order-status", auth, handler); err != nil {
		t.Fatalf("RegisterSecureRoute: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 1 {
		t.Fatalf("routes = %+v", routes)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
	integrationhttp.SignRequest(req, body, "secret", "hmac", now.Unix(), "nonce-secure")
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
	req2.Header.Set(extapi.IntegrationHeaderAPIKey, "wrong")
	routes[0].Handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status = %d", rec2.Code)
	}
}

func TestApp_Integration_RegisterSecureRoute_Idempotency(t *testing.T) {
	app := &plugin.App{Logger: logger.NewWithWriter(io.Discard, "error")}
	app.SetIntegrationIdempotencyStore(integrationhttp.NewMemoryIdempotencyStore())
	replay := integrationhttp.NewMemoryReplayStore()
	now := time.Unix(1_700_000_000, 0)
	body := []byte(`{"order_id":"100"}`)
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		integrationhttp.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	})
	auth := integrationhttp.AuthConfig{
		APIKey:      "secret",
		HMACSecret:  "hmac",
		ReplayStore: replay,
		Now:         func() time.Time { return now },
	}
	if err := app.Integration("acme").RegisterSecureRoute("POST", "order-status", auth, handler); err != nil {
		t.Fatalf("RegisterSecureRoute: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 1 {
		t.Fatalf("routes = %+v", routes)
	}

	makeReq := func(nonce string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/acme/order-status", bytes.NewReader(body))
		req.Header.Set(extapi.IntegrationHeaderIdempotencyKey, "erp-key-1")
		integrationhttp.SignRequest(req, body, "secret", "hmac", now.Unix(), nonce)
		return req
	}

	rec1 := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec1, makeReq("nonce-idem-1"))
	rec2 := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec2, makeReq("nonce-idem-2"))
	if rec1.Code != http.StatusAccepted || rec2.Code != http.StatusAccepted {
		t.Fatalf("statuses = %d %d", rec1.Code, rec2.Code)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	if rec2.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatalf("missing replay header")
	}
}

func TestApp_Integration_RegisterRoute_InvalidSlug(t *testing.T) {
	app := &plugin.App{}
	err := app.Integration("Bad Slug").RegisterRoute("POST", "x", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err == nil {
		t.Fatal("expected error for invalid slug")
	}
}
