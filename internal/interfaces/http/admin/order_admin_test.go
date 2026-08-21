package admin_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/order"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// ── helpers ─────────────────────────────────────────────────────────────

func orderAdminSetup() (*stubOrderRepo, *http.ServeMux) {
	repo := newStubOrderRepo()
	handler := admin.NewOrderAdminHandler(repo, logger.New("error"), nil)

	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/orders", requireAdmin(handler.List()))
	mux.Handle("GET /api/v1/admin/orders/{orderId}", requireAdmin(handler.Get()))
	mux.Handle("PUT /api/v1/admin/orders/{orderId}", requireAdmin(handler.Update()))
	return repo, mux
}

type auditSinkRecord struct {
	event   string
	context map[string]interface{}
}

type auditSink struct {
	records []auditSinkRecord
}

func (s *auditSink) Info(event string, ctx map[string]interface{}) {
	s.records = append(s.records, auditSinkRecord{event: event, context: cloneAuditContext(ctx)})
}

func (s *auditSink) Warn(event string, ctx map[string]interface{}) {
	s.records = append(s.records, auditSinkRecord{event: event, context: cloneAuditContext(ctx)})
}

func (s *auditSink) Error(event string, err error, ctx map[string]interface{}) {
	context := cloneAuditContext(ctx)
	if err != nil {
		context["error"] = err.Error()
	}
	s.records = append(s.records, auditSinkRecord{event: event, context: context})
}

func (s *auditSink) Last(t *testing.T) auditSinkRecord {
	t.Helper()
	if len(s.records) == 0 {
		t.Fatal("expected at least one audit record")
	}
	return s.records[len(s.records)-1]
}

func cloneAuditContext(ctx map[string]interface{}) map[string]interface{} {
	if ctx == nil {
		return map[string]interface{}{}
	}
	cloned := make(map[string]interface{}, len(ctx))
	for key, value := range ctx {
		cloned[key] = value
	}
	return cloned
}

func orderAdminSetupWithAudit(repo order.OrderRepository, sink logger.Logger) *http.ServeMux {
	handler := admin.NewOrderAdminHandlerWithAuditor(repo, adminapp.NewAuditor(sink), nil)
	requireAdmin := shophttp.RequireRole(identity.RoleAdmin)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/orders", withAdminContext(requireAdmin(handler.List())))
	mux.Handle("GET /api/v1/admin/orders/{orderId}", withAdminContext(requireAdmin(handler.Get())))
	mux.Handle("PUT /api/v1/admin/orders/{orderId}", withAdminContext(requireAdmin(handler.Update())))
	return mux
}

type failingListOrderRepo struct {
	*stubOrderRepo
	listErr error
}

func (r *failingListOrderRepo) List(_ context.Context, offset, limit int) ([]order.Order, error) {
	return nil, r.listErr
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

// ── Audit Scope Context Tests ───────────────────────────────────────────

// TestOrderAdminHandler_Update_WithScopeContext verifies that admin scope context
// (store_id, language, currency) is correctly extracted from headers and will be
// included in the emitted audit log context via the handler's scope detail injection.
func TestOrderAdminHandler_Update_WithScopeContext(t *testing.T) {
	repo := newStubOrderRepo()
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"confirmed"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")

	// Inject scope context via header (populated by upstream auth middleware)
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")

	mux.ServeHTTP(rec, req)

	// Verify the update succeeds even with scope context present
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify order status was updated
	o := parseAdminOrderResp(t, rec)
	if o["status"] != "confirmed" {
		t.Fatalf("status = %v, want confirmed", o["status"])
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
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

func TestOrderAdminHandler_List_ErrorAuditIncludesPagination(t *testing.T) {
	repo := &failingListOrderRepo{stubOrderRepo: newStubOrderRepo(), listErr: errors.New("list failed")}
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders?offset=4&limit=9", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["detail_offset"]; got != 4 {
		t.Errorf("detail_offset = %v, want %d", got, 4)
	}
	if got := entry.context["detail_limit"]; got != 9 {
		t.Errorf("detail_limit = %v, want %d", got, 9)
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

func TestOrderAdminHandler_List_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := newStubOrderRepo()
	seedOrder(t, repo, "ord-1", "cust-1")
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders?offset=0&limit=10", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["detail_offset"]; got != 0 {
		t.Errorf("detail_offset = %v, want %d", got, 0)
	}
	if got := entry.context["detail_limit"]; got != 10 {
		t.Errorf("detail_limit = %v, want %d", got, 10)
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

func TestOrderAdminHandler_List_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &failingListOrderRepo{stubOrderRepo: newStubOrderRepo(), listErr: errors.New("list failed")}
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders?offset=0&limit=10", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["detail_offset"]; got != 0 {
		t.Errorf("detail_offset = %v, want %d", got, 0)
	}
	if got := entry.context["detail_limit"]; got != 10 {
		t.Errorf("detail_limit = %v, want %d", got, 10)
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

func TestOrderAdminHandler_Get_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := newStubOrderRepo()
	seedOrder(t, repo, "ord-1", "cust-1")
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
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

func TestOrderAdminHandler_Get_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := newStubOrderRepo()
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/orders/ord-not-found", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}

	// No audit record on 404 to prevent enumeration
	if len(sink.records) != 0 {
		t.Fatalf("audit records = %d, want 0 on 404", len(sink.records))
	}
}

func TestOrderAdminHandler_Update_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := newStubOrderRepo()
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"confirmed"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
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

func TestOrderAdminHandler_Update_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := newStubOrderRepo()
	sink := &auditSink{}
	mux := orderAdminSetupWithAudit(repo, sink)
	seedOrder(t, repo, "ord-1", "cust-1")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/orders/ord-1", strings.NewReader(`{"status":"invalid"}`))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
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
