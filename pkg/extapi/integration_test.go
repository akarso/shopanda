package extapi_test

import (
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

func TestIntegrationRoutePattern(t *testing.T) {
	got, err := extapi.IntegrationRoutePattern("acme", "post", "order-status")
	if err != nil {
		t.Fatalf("IntegrationRoutePattern: %v", err)
	}
	want := "POST /api/v1/integrations/acme/order-status"
	if got != want {
		t.Fatalf("pattern = %q, want %q", got, want)
	}
}

func TestIntegrationRoutePattern_RejectsInvalidSlug(t *testing.T) {
	if _, err := extapi.IntegrationRoutePattern("Acme/ERP", "POST", "x"); err == nil {
		t.Fatal("expected error for invalid slug")
	}
}

func TestIntegrationRoutePattern_RejectsTraversal(t *testing.T) {
	if _, err := extapi.IntegrationRoutePattern("acme", "POST", "../secret"); err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestIntegrationAdminRoutePattern(t *testing.T) {
	got, err := extapi.IntegrationAdminRoutePattern("acme", "GET", "health")
	if err != nil {
		t.Fatalf("IntegrationAdminRoutePattern: %v", err)
	}
	want := "GET /api/v1/admin/integrations/acme/health"
	if got != want {
		t.Fatalf("pattern = %q, want %q", got, want)
	}
}
