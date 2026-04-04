package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/domain/order"
	"github.com/akarso/shopanda/internal/domain/shared"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
)

// ── stub ────────────────────────────────────────────────────────────────

type stubOrderRepo struct {
	orders map[string]*order.Order
}

func newStubOrderRepo() *stubOrderRepo {
	return &stubOrderRepo{orders: make(map[string]*order.Order)}
}

func (r *stubOrderRepo) FindByID(_ context.Context, id string) (*order.Order, error) {
	o, ok := r.orders[id]
	if !ok {
		return nil, nil
	}
	return o, nil
}

func (r *stubOrderRepo) FindByCustomerID(_ context.Context, customerID string) ([]order.Order, error) {
	var out []order.Order
	for _, o := range r.orders {
		if o.CustomerID == customerID {
			out = append(out, *o)
		}
	}
	return out, nil
}

func (r *stubOrderRepo) Save(_ context.Context, o *order.Order) error {
	r.orders[o.ID] = o
	return nil
}

func (r *stubOrderRepo) UpdateStatus(_ context.Context, _ *order.Order) error { return nil }

// ── helpers ─────────────────────────────────────────────────────────────

func orderSetup() (*stubOrderRepo, *http.ServeMux) {
	repo := newStubOrderRepo()
	handler := shophttp.NewOrderHandler(repo)

	requireAuth := shophttp.RequireAuth()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/orders", requireAuth(handler.List()))
	mux.Handle("GET /api/v1/orders/{orderId}", requireAuth(handler.Get()))
	return repo, mux
}

func seedOrder(t *testing.T, repo *stubOrderRepo, id, customerID string) *order.Order {
	t.Helper()
	items := []order.Item{
		{
			VariantID: "var-1",
			SKU:       "SKU-001",
			Name:      "Widget",
			Quantity:  2,
			UnitPrice: shared.MustNewMoney(1500, "EUR"),
			CreatedAt: time.Now().UTC(),
		},
	}
	o, err := order.NewOrder(id, customerID, "EUR", items)
	if err != nil {
		t.Fatalf("seedOrder: %v", err)
	}
	repo.orders[id] = &o
	return &o
}

func parseOrderBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return body
}

// ── GET /api/v1/orders/{orderId} ────────────────────────────────────────

func TestOrderHandler_Get_OK(t *testing.T) {
	repo, mux := orderSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders/ord-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseOrderBody(t, rec)
	data := body["data"].(map[string]interface{})
	o := data["order"].(map[string]interface{})

	if o["id"] != "ord-1" {
		t.Errorf("id = %v, want ord-1", o["id"])
	}
	if o["customer_id"] != "cust-1" {
		t.Errorf("customer_id = %v, want cust-1", o["customer_id"])
	}
	if o["status"] != "pending" {
		t.Errorf("status = %v, want pending", o["status"])
	}
	if o["currency"] != "EUR" {
		t.Errorf("currency = %v, want EUR", o["currency"])
	}
	if o["total_amount"].(float64) != 3000 {
		t.Errorf("total_amount = %v, want 3000", o["total_amount"])
	}

	items := o["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item := items[0].(map[string]interface{})
	if item["variant_id"] != "var-1" {
		t.Errorf("variant_id = %v, want var-1", item["variant_id"])
	}
	if item["quantity"].(float64) != 2 {
		t.Errorf("quantity = %v, want 2", item["quantity"])
	}
}

func TestOrderHandler_Get_NotFound(t *testing.T) {
	_, mux := orderSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders/ord-999", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestOrderHandler_Get_WrongCustomer(t *testing.T) {
	repo, mux := orderSetup()
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders/ord-1", nil)
	req = testhelper.CustomerRequest(req, "cust-other")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestOrderHandler_Get_Unauthenticated(t *testing.T) {
	_, mux := orderSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders/ord-1", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// ── GET /api/v1/orders ──────────────────────────────────────────────────

func TestOrderHandler_List_OK(t *testing.T) {
	repo, mux := orderSetup()
	seedOrder(t, repo, "ord-1", "cust-1")
	seedOrder(t, repo, "ord-2", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseOrderBody(t, rec)
	data := body["data"].(map[string]interface{})
	orders := data["orders"].([]interface{})

	if len(orders) != 2 {
		t.Errorf("orders len = %d, want 2", len(orders))
	}
}

func TestOrderHandler_List_Empty(t *testing.T) {
	_, mux := orderSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseOrderBody(t, rec)
	data := body["data"].(map[string]interface{})
	orders := data["orders"].([]interface{})

	if len(orders) != 0 {
		t.Errorf("orders len = %d, want 0", len(orders))
	}
}

func TestOrderHandler_List_Unauthenticated(t *testing.T) {
	_, mux := orderSetup()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestOrderHandler_List_OnlyOwnOrders(t *testing.T) {
	repo, mux := orderSetup()
	seedOrder(t, repo, "ord-1", "cust-1")
	seedOrder(t, repo, "ord-2", "cust-2")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/orders", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	body := parseOrderBody(t, rec)
	data := body["data"].(map[string]interface{})
	orders := data["orders"].([]interface{})

	if len(orders) != 1 {
		t.Errorf("orders len = %d, want 1", len(orders))
	}
	o := orders[0].(map[string]interface{})
	if o["customer_id"] != "cust-1" {
		t.Errorf("customer_id = %v, want cust-1", o["customer_id"])
	}
}
