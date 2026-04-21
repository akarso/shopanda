package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	mux.Handle("PUT /api/v1/admin/orders/{orderId}", requireAdmin(handler.Update()))
	return repo, mux
}

// parseAdminOrdersResp extracts the orders array from the response envelope.
func parseAdminOrdersResp(t *testing.T, rec *httptest.ResponseRecorder) []map[string]interface{} {
	t.Helper()
	var envelope struct {
		Data struct {
			Orders []map[string]interface{} `json:"orders"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return envelope.Data.Orders
}

// parseAdminOrderResp extracts a single order from the response envelope.
func parseAdminOrderResp(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var envelope struct {
		Data struct {
			Order map[string]interface{} `json:"order"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Data.Order == nil {
		t.Fatal("order is nil in response")
	}
	return envelope.Data.Order
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

	orders := parseAdminOrdersResp(t, rec)
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

	orders := parseAdminOrdersResp(t, rec)
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

	orders := parseAdminOrdersResp(t, rec)
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

func TestOrderAdminHandler_List_Pagination(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")
	seedOrder(t, repo, "ord-2", "cust-2")
	seedOrder(t, repo, "ord-3", "cust-3")

	// offset=1, limit=1 → exactly 1 result (proves limit is applied).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders?offset=1&limit=1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	orders := parseAdminOrdersResp(t, rec)
	if len(orders) != 1 {
		t.Fatalf("orders len = %d, want 1", len(orders))
	}

	// offset beyond total → empty list (proves offset is applied).
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/v1/admin/orders?offset=10&limit=5", nil)
	req2 = testhelper.AdminRequest(req2, "admin-1")
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("out-of-range: status = %d, want %d; body: %s", rec2.Code, http.StatusOK, rec2.Body.String())
	}

	orders2 := parseAdminOrdersResp(t, rec2)
	if len(orders2) != 0 {
		t.Fatalf("out-of-range: orders len = %d, want 0", len(orders2))
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

	o := parseAdminOrderResp(t, rec)
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

func TestOrderAdminHandler_Get_GuestUnauthorized(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-1", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// ── PUT /api/v1/admin/orders/{orderId} ──────────────────────────────────

func TestOrderAdminHandler_Update_OK(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"confirmed"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	o := parseAdminOrderResp(t, rec)
	if o["status"] != "confirmed" {
		t.Fatalf("status = %v, want confirmed", o["status"])
	}
}

func TestOrderAdminHandler_Update_InvalidTransition(t *testing.T) {
	repo, mux := orderAdminSetup()
	o := seedOrder(t, repo, "ord-1", "cust-1")
	if err := o.Confirm(); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if err := o.MarkPaid(); err != nil {
		t.Fatalf("mark paid: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"confirmed"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestOrderAdminHandler_Update_InvalidStatus(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"bogus"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestOrderAdminHandler_Update_PendingNotAllowed(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"pending"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
}

func TestOrderAdminHandler_Update_CustomerForbidden(t *testing.T) {
	repo, mux := orderAdminSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"confirmed"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}
