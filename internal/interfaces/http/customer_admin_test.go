package http_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/customer"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type mockAdminCustomerRepo struct {
	listCustomersFn func(ctx context.Context, offset, limit int) ([]customer.Customer, error)
}

func (m *mockAdminCustomerRepo) FindByID(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}

func (m *mockAdminCustomerRepo) FindByEmail(_ context.Context, _ string) (*customer.Customer, error) {
	return nil, nil
}

func (m *mockAdminCustomerRepo) Create(_ context.Context, _ *customer.Customer) error {
	return nil
}

func (m *mockAdminCustomerRepo) Update(_ context.Context, _ *customer.Customer) error {
	return nil
}

func (m *mockAdminCustomerRepo) ListCustomers(ctx context.Context, offset, limit int) ([]customer.Customer, error) {
	if m.listCustomersFn != nil {
		return m.listCustomersFn(ctx, offset, limit)
	}
	return nil, nil
}

func (m *mockAdminCustomerRepo) BumpTokenGeneration(_ context.Context, _ string) error {
	return nil
}

func (m *mockAdminCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockAdminCustomerRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func newCustomerAdminRouterWithAudit(h *shophttp.CustomerAdminHandler) *http.ServeMux {
	requireCustomersRead := shophttp.RequirePermission(rbac.CustomersRead)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/customers", withAdminContext(requireCustomersRead(h.List())))
	return mux
}

func TestCustomerAdminHandler_List_AuditIncludesScopeContext(t *testing.T) {
	now := time.Date(2026, time.May, 12, 10, 0, 0, 0, time.UTC)
	repo := &mockAdminCustomerRepo{
		listCustomersFn: func(_ context.Context, offset, limit int) ([]customer.Customer, error) {
			if offset != 2 {
				t.Errorf("offset = %d, want 2", offset)
			}
			if limit != 7 {
				t.Errorf("limit = %d, want 7", limit)
			}
			c, err := customer.NewCustomer("cust-1", "cust@example.com")
			if err != nil {
				t.Fatalf("new customer: %v", err)
			}
			c.FirstName = "Ada"
			c.LastName = "Lovelace"
			c.Role = customer.RoleCustomer
			c.Status = customer.StatusActive
			c.CreatedAt = now
			c.UpdatedAt = now
			c.MarkEmailVerified()
			return []customer.Customer{c}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers?offset=2&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data = %#v, want object", body["data"])
	}
	customers, ok := data["customers"].([]interface{})
	if !ok {
		t.Fatalf("customers = %#v, want array", data["customers"])
	}
	if len(customers) != 1 {
		t.Fatalf("len(customers) = %d, want 1", len(customers))
	}
	item, ok := customers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("customer[0] = %#v, want object", customers[0])
	}
	if got := item["id"]; got != "cust-1" {
		t.Errorf("id = %v, want %q", got, "cust-1")
	}
	if got := item["email"]; got != "cust@example.com" {
		t.Errorf("email = %v, want %q", got, "cust@example.com")
	}
	if got := item["first_name"]; got != "Ada" {
		t.Errorf("first_name = %v, want %q", got, "Ada")
	}
	if got := item["last_name"]; got != "Lovelace" {
		t.Errorf("last_name = %v, want %q", got, "Lovelace")
	}
	if got := item["role"]; got != string(customer.RoleCustomer) {
		t.Errorf("role = %v, want %q", got, customer.RoleCustomer)
	}
	if got := item["status"]; got != string(customer.StatusActive) {
		t.Errorf("status = %v, want %q", got, customer.StatusActive)
	}
	if got := item["email_verified_at"]; got == nil || got == "" {
		t.Errorf("email_verified_at = %v, want non-empty", got)
	}
	if got := item["created_at"]; got != now.Format(time.RFC3339) {
		t.Errorf("created_at = %v, want %q", got, now.Format(time.RFC3339))
	}
	if got := item["updated_at"]; got == nil || got == "" {
		t.Errorf("updated_at = %v, want non-empty", got)
	}
	if got := item["updated_at"]; got != item["email_verified_at"] {
		t.Errorf("updated_at = %v, want match email_verified_at %v", got, item["email_verified_at"])
	}
	if _, ok := item["password_hash"]; ok {
		t.Errorf("password_hash present = %v, want absent", item["password_hash"])
	}
	if _, ok := item["token_generation"]; ok {
		t.Errorf("token_generation present = %v, want absent", item["token_generation"])
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRead)
	}
	if got := entry.context["resource_type"]; got != "customers" {
		t.Errorf("resource_type = %v, want %q", got, "customers")
	}
	if got := entry.context["detail_offset"]; got != 2 {
		t.Errorf("detail_offset = %v, want %d", got, 2)
	}
	if got := entry.context["detail_limit"]; got != 7 {
		t.Errorf("detail_limit = %v, want %d", got, 7)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
}

func TestCustomerAdminHandler_List_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		listCustomersFn: func(_ context.Context, offset, limit int) ([]customer.Customer, error) {
			return nil, errors.New("customers unavailable")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRead)
	}
	if got := entry.context["resource_type"]; got != "customers" {
		t.Errorf("resource_type = %v, want %q", got, "customers")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
}

func TestCustomerAdminHandler_List_AuditInvalidPaginationIncludesError(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		listCustomersFn: func(_ context.Context, offset, limit int) ([]customer.Customer, error) {
			t.Fatalf("ListCustomers called with offset=%d limit=%d", offset, limit)
			return nil, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers?offset=bad&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRead)
	}
	if got := entry.context["resource_type"]; got != "customers" {
		t.Errorf("resource_type = %v, want %q", got, "customers")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "validation: offset must be a non-negative integer" {
		t.Errorf("error = %v, want %q", got, "validation: offset must be a non-negative integer")
	}
	if got := entry.context["detail_store_id"]; got != "store-eu" {
		t.Errorf("detail_store_id = %v, want %q", got, "store-eu")
	}
	if got := entry.context["detail_language"]; got != "en" {
		t.Errorf("detail_language = %v, want %q", got, "en")
	}
	if got := entry.context["detail_currency"]; got != "EUR" {
		t.Errorf("detail_currency = %v, want %q", got, "EUR")
	}
	if _, ok := entry.context["detail_offset"]; ok {
		t.Errorf("detail_offset present = %v, want absent", entry.context["detail_offset"])
	}
	if _, ok := entry.context["detail_limit"]; ok {
		t.Errorf("detail_limit present = %v, want absent", entry.context["detail_limit"])
	}
}

func TestCustomerAdminHandler_List_CustomerForbidden(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCustomerAdminHandler_List_GuestUnauthorized(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers", nil)
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

type nilWriter struct{}

func (nilWriter) Write(p []byte) (n int, err error) { return len(p), nil }
