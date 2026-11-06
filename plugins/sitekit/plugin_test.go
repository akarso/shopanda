package sitekit_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/platform/plugin"
	sitekit "github.com/akarso/shopanda/plugins/sitekit"
)

func TestName(t *testing.T) {
	t.Parallel()
	if got := sitekit.New().Name(); got != "sitekit" {
		t.Fatalf("Name() = %q, want sitekit", got)
	}
}

func TestInitRegistersHealth(t *testing.T) {
	t.Parallel()
	app := &plugin.App{}
	if err := sitekit.New().Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	routes := app.PublicRoutes()
	if len(routes) != 1 || routes[0].Pattern != "GET /api/sitekit/health" {
		t.Fatalf("PublicRoutes = %+v, want health route", routes)
	}
	rec := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sitekit/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}
