package plugin_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/internal/platform/plugin"
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

func TestApp_Integration_RegisterRoute_InvalidSlug(t *testing.T) {
	app := &plugin.App{}
	err := app.Integration("Bad Slug").RegisterRoute("POST", "x", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if err == nil {
		t.Fatal("expected error for invalid slug")
	}
}
