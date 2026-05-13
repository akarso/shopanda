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
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

type mockAdminCustomerRepo struct {
	bumpTokenGenerationFn func(ctx context.Context, customerID string) error
	deleteAccountFn       func(ctx context.Context, customerID string) error
	findByIDFn            func(ctx context.Context, id string) (*customer.Customer, error)
	listCustomersFn       func(ctx context.Context, offset, limit int) ([]customer.Customer, error)
}

func (m *mockAdminCustomerRepo) FindByID(ctx context.Context, id string) (*customer.Customer, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
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

func (m *mockAdminCustomerRepo) BumpTokenGeneration(ctx context.Context, customerID string) error {
	if m.bumpTokenGenerationFn != nil {
		return m.bumpTokenGenerationFn(ctx, customerID)
	}
	return nil
}

func (m *mockAdminCustomerRepo) ChangePasswordAndBumpTokenGeneration(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockAdminCustomerRepo) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockAdminCustomerRepo) DeleteAccount(ctx context.Context, customerID string) error {
	if m.deleteAccountFn != nil {
		return m.deleteAccountFn(ctx, customerID)
	}
	return nil
}

func newCustomerAdminRouterWithAudit(h *shophttp.CustomerAdminHandler) *http.ServeMux {
	requireCustomersRead := shophttp.RequirePermission(rbac.CustomersRead)
	requireCustomersWrite := shophttp.RequirePermission(rbac.CustomersWrite)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/customers", withAdminContext(requireCustomersRead(h.List())))
	mux.Handle("GET /api/v1/admin/customers/{customerId}", withAdminContext(requireCustomersRead(h.Get())))
	mux.Handle("DELETE /api/v1/admin/customers/{customerId}", withAdminContext(requireCustomersWrite(h.Delete())))
	mux.Handle("POST /api/v1/admin/customers/{customerId}/revoke-sessions", withAdminContext(requireCustomersWrite(h.RevokeSessions())))
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

func TestCustomerAdminHandler_List_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		listCustomersFn: func(_ context.Context, offset, limit int) ([]customer.Customer, error) {
			return []customer.Customer{{
				ID:        "cust-1",
				Email:     "cust@example.com",
				FirstName: "Ada",
				LastName:  "Lovelace",
				Role:      customer.RoleCustomer,
				Status:    customer.StatusActive,
			}}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers?offset=2&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRead)
	}
	if got := entry.context["detail_offset"]; got != 2 {
		t.Errorf("detail_offset = %v, want %d", got, 2)
	}
	if got := entry.context["detail_limit"]; got != 7 {
		t.Errorf("detail_limit = %v, want %d", got, 7)
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["resource_type"]; got != "customers" {
		t.Errorf("resource_type = %v, want %q", got, "customers")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
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

func TestCustomerAdminHandler_Get_AuditIncludesScopeContext(t *testing.T) {
	now := time.Date(2026, time.May, 12, 10, 0, 0, 0, time.UTC)
	repo := &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, id string) (*customer.Customer, error) {
			if id != "cust-1" {
				t.Errorf("id = %q, want %q", id, "cust-1")
			}
			c := &customer.Customer{
				ID:        "cust-1",
				Email:     "cust@example.com",
				FirstName: "Ada",
				LastName:  "Lovelace",
				Role:      customer.RoleCustomer,
				Status:    customer.StatusActive,
				CreatedAt: now,
				UpdatedAt: now,
			}
			return c, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-1", nil)
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
	item, ok := data["customer"].(map[string]interface{})
	if !ok {
		t.Fatalf("customer = %#v, want object", data["customer"])
	}
	if got := item["id"]; got != "cust-1" {
		t.Errorf("id = %v, want %q", got, "cust-1")
	}
	if got := item["first_name"]; got != "Ada" {
		t.Errorf("first_name = %v, want %q", got, "Ada")
	}
	if got := item["last_name"]; got != "Lovelace" {
		t.Errorf("last_name = %v, want %q", got, "Lovelace")
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
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
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

func TestCustomerAdminHandler_Get_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, id string) (*customer.Customer, error) {
			return &customer.Customer{
				ID:        "cust-1",
				Email:     "cust@example.com",
				FirstName: "Ada",
				LastName:  "Lovelace",
				Role:      customer.RoleCustomer,
				Status:    customer.StatusActive,
			}, nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRead)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
	}
}

func TestCustomerAdminHandler_Get_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, id string) (*customer.Customer, error) {
			return nil, errors.New("customer lookup failed")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-1", nil)
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
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "customer lookup failed" {
		t.Errorf("error = %v, want %q", got, "customer lookup failed")
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

func TestCustomerAdminHandler_Get_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		findByIDFn: func(_ context.Context, id string) (*customer.Customer, error) {
			return nil, errors.New("customer lookup failed")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
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
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "customer lookup failed" {
		t.Errorf("error = %v, want %q", got, "customer lookup failed")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
}

func TestCustomerAdminHandler_Get_NotFound(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-404", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(sink.records) != 0 {
		t.Fatalf("audit records = %d, want 0", len(sink.records))
	}
}

func TestCustomerAdminHandler_Get_CustomerForbidden(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCustomerAdminHandler_Get_GuestUnauthorized(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/customers/cust-1", nil)
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestCustomerAdminHandler_RevokeSessions_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		bumpTokenGenerationFn: func(_ context.Context, customerID string) error {
			if customerID != "cust-1" {
				t.Errorf("customerID = %q, want %q", customerID, "cust-1")
			}
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-1/revoke-sessions", nil)
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
	if got := data["customer_id"]; got != "cust-1" {
		t.Errorf("customer_id = %v, want %q", got, "cust-1")
	}
	if got := data["status"]; got != "sessions_revoked" {
		t.Errorf("status = %v, want %q", got, "sessions_revoked")
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRevoke {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRevoke)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
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

func TestCustomerAdminHandler_RevokeSessions_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		bumpTokenGenerationFn: func(_ context.Context, customerID string) error {
			if customerID != "cust-1" {
				t.Errorf("customerID = %q, want %q", customerID, "cust-1")
			}
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-1/revoke-sessions", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRevoke {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRevoke)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
	if _, ok := entry.context["error"]; ok {
		t.Errorf("error present = %v, want absent", entry.context["error"])
	}
}

func TestCustomerAdminHandler_RevokeSessions_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		bumpTokenGenerationFn: func(_ context.Context, customerID string) error {
			return errors.New("revoke failed")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-1/revoke-sessions", nil)
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
	if got := entry.context["action"]; got != adminapp.AuditCustomerRevoke {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRevoke)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "revoke failed" {
		t.Errorf("error = %v, want %q", got, "revoke failed")
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

func TestCustomerAdminHandler_RevokeSessions_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		bumpTokenGenerationFn: func(_ context.Context, customerID string) error {
			return errors.New("revoke failed")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-1/revoke-sessions", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerRevoke {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerRevoke)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "revoke failed" {
		t.Errorf("error = %v, want %q", got, "revoke failed")
	}
	if _, ok := entry.context["detail_store_id"]; ok {
		t.Errorf("detail_store_id present = %v, want absent", entry.context["detail_store_id"])
	}
	if _, ok := entry.context["detail_language"]; ok {
		t.Errorf("detail_language present = %v, want absent", entry.context["detail_language"])
	}
	if _, ok := entry.context["detail_currency"]; ok {
		t.Errorf("detail_currency present = %v, want absent", entry.context["detail_currency"])
	}
}

func TestCustomerAdminHandler_RevokeSessions_NotFound(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		bumpTokenGenerationFn: func(_ context.Context, customerID string) error {
			return apperror.NotFound("customer not found")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-404/revoke-sessions", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(sink.records) != 0 {
		t.Fatalf("audit records = %d, want 0", len(sink.records))
	}
}

func TestCustomerAdminHandler_RevokeSessions_CustomerForbidden(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-1/revoke-sessions", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCustomerAdminHandler_RevokeSessions_GuestUnauthorized(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditor(repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/customers/cust-1/revoke-sessions", nil)
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestCustomerAdminHandler_Delete_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		deleteAccountFn: func(_ context.Context, customerID string) error {
			if customerID != "cust-1" {
				t.Errorf("customerID = %q, want %q", customerID, "cust-1")
			}
			return nil
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(repo, repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/customers/cust-1", nil)
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
	if got := data["deleted"]; got != true {
		t.Errorf("deleted = %v, want true", got)
	}
	if got := data["customer_id"]; got != "cust-1" {
		t.Errorf("customer_id = %v, want %q", got, "cust-1")
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditCustomerDelete {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerDelete)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
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

func TestCustomerAdminHandler_Delete_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		deleteAccountFn: func(_ context.Context, customerID string) error {
			return errors.New("delete failed")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(repo, repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/customers/cust-1", nil)
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
	if got := entry.context["action"]; got != adminapp.AuditCustomerDelete {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerDelete)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "delete failed" {
		t.Errorf("error = %v, want %q", got, "delete failed")
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

func TestCustomerAdminHandler_Delete_MisconfiguredAuditsFailure(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(repo, nil, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/customers/cust-1", nil)
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
	if got := entry.context["action"]; got != adminapp.AuditCustomerDelete {
		t.Errorf("action = %v, want %q", got, adminapp.AuditCustomerDelete)
	}
	if got := entry.context["resource_type"]; got != "customer" {
		t.Errorf("resource_type = %v, want %q", got, "customer")
	}
	if got := entry.context["resource_id"]; got != "cust-1" {
		t.Errorf("resource_id = %v, want %q", got, "cust-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "customer delete is not configured" {
		t.Errorf("error = %v, want %q", got, "customer delete is not configured")
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

func TestCustomerAdminHandler_Delete_NotFound(t *testing.T) {
	repo := &mockAdminCustomerRepo{
		deleteAccountFn: func(_ context.Context, customerID string) error {
			return apperror.NotFound("customer not found")
		},
	}
	sink := &auditSink{}
	h := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(repo, repo, adminapp.NewAuditor(sink))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/customers/cust-404", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	if len(sink.records) != 0 {
		t.Fatalf("audit records = %d, want 0", len(sink.records))
	}
}

func TestCustomerAdminHandler_Delete_CustomerForbidden(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(repo, repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/customers/cust-1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestCustomerAdminHandler_Delete_GuestUnauthorized(t *testing.T) {
	repo := &mockAdminCustomerRepo{}
	h := shophttp.NewCustomerAdminHandlerWithAuditorAndDeleter(repo, repo, adminapp.NewAuditor(logger.NewWithWriter(nilWriter{}, "info")))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/admin/customers/cust-1", nil)
	newCustomerAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

type nilWriter struct{}

func (nilWriter) Write(p []byte) (n int, err error) { return len(p), nil }
