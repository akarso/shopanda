package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/identity"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

// ── helpers ─────────────────────────────────────────────────────────────

func orderAdminSetup() (*stubOrderRepo, *http.ServeMux) {
	repo := newStubOrderRepo()
	handler := shophttp.NewOrderAdminHandler(repo)

	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/orders", requireAdmin(handler.List()))
	mux.Handle("GET /api/v1/admin/orders/{orderId}", requireAdmin(handler.Get()))
	return repo, mux
}

// ── GET /api/v1/admin/orders ────────────────────────────────────────────

func TestOrderAdminHandler_List_OK(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")
	seedOrder(t, repo, "ord-2", "cust-2")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	orders := data["orders"].([]interface{})

	if len(orders) != 2 {
		t.Errorf("orders len = %d, want 2", len(orders))
	}
}

func TestOrderAdminHandler_List_Empty(t *testing.T) {
	_, mux := orderAdminSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	orders := data["orders"].([]interface{})

	if len(orders) != 0 {
		t.Errorf("orders len = %d, want 0", len(orders))
	}
}

func TestOrderAdminHandler_List_AllCustomers(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")
	seedOrder(t, repo, "ord-2", "cust-2")
	seedOrder(t, repo, "ord-3", "cust-3")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	orders := data["orders"].([]interface{})

	if len(orders) != 3 {
		t.Errorf("orders len = %d, want 3", len(orders))
	}
}

func TestOrderAdminHandler_List_CustomerForbidden(t *testing.T) {
	_, mux := orderAdminSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestOrderAdminHandler_List_GuestUnauthorized(t *testing.T) {
	_, mux := orderAdminSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// ── GET /api/v1/admin/orders/{orderId} ──────────────────────────────────

func TestOrderAdminHandler_Get_OK(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data := body["data"].(map[string]interface{})
	o := data["order"].(map[string]interface{})

	if o["id"] != "ord-1" {
		t.Errorf("id = %v, want ord-1", o["id"])
	}
	if o["customer_id"] != "cust-1" {
		t.Errorf("customer_id = %v, want cust-1", o["customer_id"])
	}
}

func TestOrderAdminHandler_Get_AnyCustomerOrder(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-other")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestOrderAdminHandler_Get_NotFound(t *testing.T) {
	_, mux := orderAdminSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-999", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOrderAdminHandler_Get_CustomerForbidden(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
