package pluginsdk_test

import (
	"net/http"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/platform/auth"
	"github.com/akarso/shopanda/pkg/pluginsdk"
)

func TestCustomerID(t *testing.T) {
	if got := pluginsdk.CustomerID(nil); got != "" {
		t.Fatalf("nil request = %q, want empty", got)
	}

	req, _ := http.NewRequest("GET", "/", nil)
	if got := pluginsdk.CustomerID(req); got != "" {
		t.Fatalf("empty context = %q, want empty", got)
	}

	guest := req.WithContext(auth.WithIdentity(req.Context(), identity.Guest()))
	if got := pluginsdk.CustomerID(guest); got != "" {
		t.Fatalf("guest = %q, want empty", got)
	}

	cust, err := identity.NewIdentity("cust-1", identity.RoleCustomer)
	if err != nil {
		t.Fatal(err)
	}
	customerReq := req.WithContext(auth.WithIdentity(req.Context(), cust))
	if got := pluginsdk.CustomerID(customerReq); got != "cust-1" {
		t.Fatalf("customer = %q, want cust-1", got)
	}

	admin, err := identity.NewIdentity("admin-1", identity.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	adminReq := req.WithContext(auth.WithIdentity(req.Context(), admin))
	if got := pluginsdk.CustomerID(adminReq); got != "admin-1" {
		t.Fatalf("admin storefront actor = %q, want admin-1", got)
	}
}
