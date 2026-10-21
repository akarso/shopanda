package admin_test

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/akarso/shopanda/internal/interfaces/http/admin"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"

	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
)

func init() {
	sql.Register("shopanda-admin-fake-tx", adminFakeTxDriver{})
}

type adminFakeTxDriver struct{}

func (adminFakeTxDriver) Open(string) (driver.Conn, error) { return adminFakeTxConn{}, nil }

type adminFakeTxConn struct{}

func (adminFakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (adminFakeTxConn) Close() error                        { return nil }
func (adminFakeTxConn) Begin() (driver.Tx, error)           { return adminFakeTx{}, nil }

type adminFakeTx struct{}

func (adminFakeTx) Commit() error   { return nil }
func (adminFakeTx) Rollback() error { return nil }

var (
	adminFakeTxOnce sync.Once
	adminFakeTxDB   *sql.DB
)

func adminFakeTxDBConn() *sql.DB {
	adminFakeTxOnce.Do(func() {
		db, err := sql.Open("shopanda-admin-fake-tx", "")
		if err != nil {
			panic(err)
		}
		adminFakeTxDB = db
	})
	return adminFakeTxDB
}

// --- mock for admin tests ---

type mockAdminProductRepo struct {
	findByIDFn                 func(ctx context.Context, id string) (*catalog.Product, error)
	listFn                     func(ctx context.Context, filter catalog.ListFilter) ([]catalog.Product, error)
	createFn                   func(ctx context.Context, p *catalog.Product) error
	updateFn                   func(ctx context.Context, p *catalog.Product) error
	listCategoryIDsByProductFn func(ctx context.Context, productID string) ([]string, error)
	lockForUpdateFn            func(ctx context.Context, id string) (catalog.Type, error)
	lockedType                 catalog.Type
	lockedPricingMode          catalog.PricingMode
}

type mockAdminProductRepoWithoutCategoryLookup struct {
	findByIDFn func(ctx context.Context, id string) (*catalog.Product, error)
}

func (m *mockAdminProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockAdminProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	return nil, nil
}

func (m *mockAdminProductRepo) List(ctx context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, nil
}

func (m *mockAdminProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return nil
}

func (m *mockAdminProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, p)
	}
	return nil
}
func (m *mockAdminProductRepo) ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error) {
	if m.listCategoryIDsByProductFn != nil {
		return m.listCategoryIDsByProductFn(ctx, productID)
	}
	return nil, nil
}
func (m *mockAdminProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockAdminProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

func (m *mockAdminProductRepo) LockForUpdate(ctx context.Context, id string) (catalog.Type, error) {
	if m.lockForUpdateFn != nil {
		return m.lockForUpdateFn(ctx, id)
	}
	p, err := m.FindByID(ctx, id)
	if err != nil {
		return "", err
	}
	if p == nil {
		return "", apperror.NotFound("product not found")
	}
	return p.Type, nil
}

func (m *mockAdminProductRepo) LockTypeAndPricingMode(ctx context.Context, id string) (catalog.Type, catalog.PricingMode, error) {
	typ, err := m.LockForUpdate(ctx, id)
	if err != nil {
		return "", "", err
	}
	if m.lockedType != "" {
		typ = m.lockedType
	}
	if m.lockedPricingMode != "" {
		return typ, m.lockedPricingMode, nil
	}
	product, err := m.FindByID(ctx, id)
	if err != nil {
		return "", "", err
	}
	if product == nil {
		return typ, "", nil
	}
	return typ, product.PricingMode, nil
}

type mockBundleComponentGuard struct {
	bundleCount    int
	componentCount int
}

var _ catalog.BundleComponentRepository = (*mockBundleComponentGuard)(nil)

func (m *mockBundleComponentGuard) ListByBundleID(context.Context, string) ([]catalog.BundleComponent, error) {
	return []catalog.BundleComponent{}, nil
}

func (m *mockBundleComponentGuard) CountByBundleID(context.Context, string) (int, error) {
	return m.bundleCount, nil
}

func (m *mockBundleComponentGuard) CountByComponentID(context.Context, string) (int, error) {
	return m.componentCount, nil
}

func (m *mockBundleComponentGuard) Upsert(context.Context, *catalog.BundleComponent) error {
	return nil
}

func (m *mockBundleComponentGuard) Remove(context.Context, string, string) error {
	return nil
}

func (m *mockBundleComponentGuard) WithTx(_ *sql.Tx) catalog.BundleComponentRepository {
	return m
}

type mockGroupedMemberGuard struct {
	groupCount  int
	memberCount int
}

var _ catalog.GroupedMemberRepository = (*mockGroupedMemberGuard)(nil)

func (m *mockGroupedMemberGuard) ListByGroupID(context.Context, string) ([]catalog.GroupedMember, error) {
	return []catalog.GroupedMember{}, nil
}

func (m *mockGroupedMemberGuard) CountByGroupID(context.Context, string) (int, error) {
	return m.groupCount, nil
}

func (m *mockGroupedMemberGuard) CountByMemberID(context.Context, string) (int, error) {
	return m.memberCount, nil
}

func (m *mockGroupedMemberGuard) Insert(context.Context, *catalog.GroupedMember) error {
	return nil
}

func (m *mockGroupedMemberGuard) Remove(context.Context, string, string) error {
	return nil
}

func (m *mockGroupedMemberGuard) WithTx(_ *sql.Tx) catalog.GroupedMemberRepository {
	return m
}

func (m *mockAdminProductRepoWithoutCategoryLookup) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockAdminProductRepoWithoutCategoryLookup) FindBySlug(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}

func (m *mockAdminProductRepoWithoutCategoryLookup) List(_ context.Context, _ catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}

func (m *mockAdminProductRepoWithoutCategoryLookup) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}

func (m *mockAdminProductRepoWithoutCategoryLookup) Create(_ context.Context, _ *catalog.Product) error {
	return nil
}

func (m *mockAdminProductRepoWithoutCategoryLookup) Update(_ context.Context, _ *catalog.Product) error {
	return nil
}

// --- helpers ---

func newAdminRouter(h *admin.ProductAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/admin/products", h.List())
	mux.HandleFunc("GET /api/v1/admin/products/{id}", h.Get())
	mux.HandleFunc("POST /api/v1/admin/products", h.Create())
	mux.HandleFunc("PUT /api/v1/admin/products/{id}", h.Update())
	return mux
}

func newAdminRouterWithAudit(h *admin.ProductAdminHandler) *http.ServeMux {
	requireProductsRead := admin.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := admin.RequirePermission(rbac.ProductsWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products", withAdminContext(requireProductsRead(h.List())))
	mux.Handle("GET /api/v1/admin/products/{id}", withAdminContext(requireProductsRead(h.Get())))
	mux.Handle("PUT /api/v1/admin/products/{id}", withAdminContext(requireProductsWrite(h.Update())))
	return mux
}

func newAdminCreateRouterWithAudit(h *admin.ProductAdminHandler) *http.ServeMux {
	requireProductsWrite := admin.RequirePermission(rbac.ProductsWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("POST /api/v1/admin/products", withAdminContext(requireProductsWrite(h.Create())))
	return mux
}

func testAdminBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
}

func jsonBody(t *testing.T, v interface{}) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bytes.NewReader(b)
}

// --- List tests ---

func TestProductAdminHandler_Get_OK(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id != "p1" {
				t.Fatalf("id = %q, want %q", id, "p1")
			}
			return &catalog.Product{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive, Type: catalog.TypeSimple}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, productID string) ([]string, error) {
			if productID != "p1" {
				t.Fatalf("productID = %q, want %q", productID, "p1")
			}
			return []string{"cat-1", "cat-2"}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data is not an object: %v", body)
	}
	product, ok := data["product"].(map[string]interface{})
	if !ok {
		t.Fatalf("product is not an object: %v", data)
	}
	if product["ID"] != "p1" {
		t.Fatalf("product ID = %v, want %q", product["ID"], "p1")
	}
	categoryIDs, ok := data["category_ids"].([]interface{})
	if !ok {
		t.Fatalf("category_ids is not an array: %v", data)
	}
	if len(categoryIDs) != 2 {
		t.Fatalf("category_ids len = %d, want 2", len(categoryIDs))
	}
}

func TestProductAdminHandler_Get_NotFound(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestProductAdminHandler_Get_MissingCategoryLookupCapability(t *testing.T) {
	repo := &mockAdminProductRepoWithoutCategoryLookup{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusActive, Type: catalog.TypeSimple}, nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductRead)
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got != "product repository missing category lookup capability" {
		t.Errorf("error = %v, want %q", got, "product repository missing category lookup capability")
	}
}

func TestProductAdminHandler_List_OK(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset != 0 {
				t.Errorf("offset = %d, want 0", filter.Offset)
			}
			if filter.Limit != 20 {
				t.Errorf("limit = %d, want 20", filter.Limit)
			}
			return []catalog.Product{
				{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive},
				{ID: "p2", Name: "Gadget", Slug: "gadget", Status: catalog.StatusDraft},
			}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data is not an object: %v", body)
	}
	products, ok := data["products"].([]interface{})
	if !ok {
		t.Fatalf("products is not an array: %v", data)
	}
	if len(products) != 2 {
		t.Fatalf("products len = %d, want 2", len(products))
	}
}

func TestProductAdminHandler_List_Empty(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, _ catalog.ListFilter) ([]catalog.Product, error) {
			return nil, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestProductAdminHandler_List_FilterByType(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Type != catalog.TypeBundle {
				t.Errorf("type = %q, want bundle", filter.Type)
			}
			return []catalog.Product{{ID: "b1", Name: "Bundle", Slug: "bundle", Type: catalog.TypeBundle}}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?type=bundle", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestProductAdminHandler_List_FilterByTypeWithPagination(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Type != catalog.TypeVirtual {
				t.Errorf("type = %q, want virtual", filter.Type)
			}
			if filter.Offset != 5 {
				t.Errorf("offset = %d, want 5", filter.Offset)
			}
			if filter.Limit != 3 {
				t.Errorf("limit = %d, want 3", filter.Limit)
			}
			return nil, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?type=virtual&offset=5&limit=3", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestProductAdminHandler_List_FilterByType_AuditDetails(t *testing.T) {
	sink := &auditSink{}
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			return []catalog.Product{}, nil
		},
	}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?type=simple", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	entry := sink.Last(t)
	if got := entry.context["detail_type"]; got != "simple" {
		t.Errorf("detail_type = %v, want simple", got)
	}
}

func TestProductAdminHandler_List_InvalidTypeFilter(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, _ catalog.ListFilter) ([]catalog.Product, error) {
			t.Fatal("List must not be called for invalid type filter")
			return nil, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?type=kit", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `invalid product type \"kit\"`) {
		t.Fatalf("body = %s, want invalid product type kit", rec.Body.String())
	}
}

func TestProductAdminHandler_List_EmptyTypeQueryIgnored(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Type != "" {
				t.Errorf("type = %q, want empty (whitespace query ignored)", filter.Type)
			}
			return nil, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?type=%20%20", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestProductAdminHandler_List_Pagination(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset != 10 {
				t.Errorf("offset = %d, want 10", filter.Offset)
			}
			if filter.Limit != 5 {
				t.Errorf("limit = %d, want 5", filter.Limit)
			}
			return nil, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?offset=10&limit=5", nil)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestProductAdminHandler_List_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?offset=2&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductRead)
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
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["resource_type"]; got != "products" {
		t.Errorf("resource_type = %v, want %q", got, "products")
	}
	if got := entry.context["result"]; got != "success" {
		t.Errorf("result = %v, want %q", got, "success")
	}
}

func TestProductAdminHandler_List_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products?offset=2&limit=7", nil)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductRead {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductRead)
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
	if got := entry.context["resource_type"]; got != "products" {
		t.Errorf("resource_type = %v, want %q", got, "products")
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
}

// --- Create tests ---

func TestProductAdminHandler_Create_OK(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name":        "Widget",
		"slug":        "widget",
		"description": "A fine widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created == nil {
		t.Fatal("product was not created")
	}
	if created.Name != "Widget" {
		t.Errorf("name = %q, want Widget", created.Name)
	}
	if created.Slug != "widget" {
		t.Errorf("slug = %q, want widget", created.Slug)
	}
	if created.Description != "A fine widget" {
		t.Errorf("description = %q, want 'A fine widget'", created.Description)
	}
	if created.Status != catalog.StatusDraft {
		t.Errorf("status = %q, want draft", created.Status)
	}
	if created.Type != catalog.TypeSimple {
		t.Errorf("type = %q, want simple (default)", created.Type)
	}
	if created.ID == "" {
		t.Error("product ID should be generated")
	}
}

func TestProductAdminHandler_Create_PricingMode(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithVariantPrices(adminOwnPriceVariants{}, adminOwnPrices{})

	body := jsonBody(t, map[string]interface{}{
		"name":         "Kit",
		"slug":         "kit",
		"type":         "bundle",
		"pricing_mode": "dynamic",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil || created.PricingMode != catalog.PricingModeDynamic || created.Type != catalog.TypeBundle {
		t.Fatalf("created = %+v, want bundle/dynamic", created)
	}

	body = jsonBody(t, map[string]interface{}{
		"name":         "Plain",
		"slug":         "plain",
		"type":         "simple",
		"pricing_mode": "dynamic",
	})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bundle") {
		t.Fatalf("body = %s, want bundle-only rejection", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_PricingMode(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithTxStarter(adminFakeTxDBConn()).
		WithVariantPrices(adminOwnPriceVariants{}, adminOwnPrices{})
	body := jsonBody(t, map[string]interface{}{"pricing_mode": "dynamic"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.PricingMode != catalog.PricingModeDynamic {
		t.Fatalf("mode = %+v, want dynamic", updated)
	}
}

func TestProductAdminHandler_Update_DynamicOnSimpleRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Plain", Slug: "plain", Status: catalog.StatusDraft,
				Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{"pricing_mode": "dynamic"}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "bundle") {
		t.Fatalf("body = %s, want bundle-only rejection", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeUsesLockedPricingMode(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		lockedPricingMode: catalog.PricingModeDynamic,
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn()).
		WithVariantPrices(adminOwnPriceVariants{}, adminOwnPrices{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{"type": "bundle"}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.PricingMode != catalog.PricingModeDynamic {
		t.Fatalf("mode = %+v, want locked dynamic", updated)
	}
}

func TestProductAdminHandler_Update_TypeChangeOffBundleOmitsMode(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
		Type: catalog.TypeBundle, PricingMode: catalog.PricingModeDynamic,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{"type": "simple"}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeSimple || updated.PricingMode != catalog.PricingModeFixed {
		t.Fatalf("updated = %+v, want simple/fixed", updated)
	}
}

type adminOwnPriceVariants struct {
	variants []catalog.Variant
}

func (adminOwnPriceVariants) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (adminOwnPriceVariants) FindBySKU(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (adminOwnPriceVariants) FindBySKUs(context.Context, []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (m adminOwnPriceVariants) ListByProductID(_ context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	var matched []catalog.Variant
	for _, variant := range m.variants {
		if variant.ProductID == productID {
			matched = append(matched, variant)
		}
	}
	if offset >= len(matched) {
		return nil, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(matched) {
		end = len(matched)
	}
	return matched[offset:end], nil
}
func (adminOwnPriceVariants) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (adminOwnPriceVariants) Create(context.Context, *catalog.Variant) error { return nil }
func (adminOwnPriceVariants) Update(context.Context, *catalog.Variant) error { return nil }

type adminOwnPrices struct {
	rows map[string][]pricing.Price
}

func (adminOwnPrices) FindByVariantCurrencyAndStore(context.Context, string, string, string) (*pricing.Price, error) {
	return nil, nil
}
func (adminOwnPrices) FindByVariantsCurrencyAndStore(context.Context, []string, string, string) (map[string]*pricing.Price, error) {
	return nil, nil
}
func (m adminOwnPrices) ListByVariantID(_ context.Context, variantID string) ([]pricing.Price, error) {
	return m.rows[variantID], nil
}
func (adminOwnPrices) List(context.Context, int, int) ([]pricing.Price, error) { return nil, nil }
func (adminOwnPrices) Upsert(context.Context, *pricing.Price) error            { return nil }

func TestProductAdminHandler_Update_DynamicRejectsOwnPrices(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Plain", Slug: "plain", Status: catalog.StatusActive,
		Type: catalog.TypeSimple, PricingMode: catalog.PricingModeFixed,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			cp := *p
			existing = &cp
			return nil
		},
	}
	prices := adminOwnPrices{rows: map[string][]pricing.Price{
		"v1": {{ID: "pr1", VariantID: "v1"}},
	}}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn()).
		WithVariantPrices(adminOwnPriceVariants{variants: []catalog.Variant{{ID: "v1", ProductID: "p1"}}}, prices)

	body := map[string]interface{}{"type": "bundle", "pricing_mode": "dynamic"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, body))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "remove bundle prices") {
		t.Fatalf("body = %s, want own-price rejection", rec.Body.String())
	}
	if updated != nil {
		t.Fatalf("updated = %+v, want no write while prices exist", updated)
	}

	delete(prices.rows, "v1")
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, body))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after delete = %d, body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeBundle || updated.PricingMode != catalog.PricingModeDynamic {
		t.Fatalf("updated = %+v, want bundle/dynamic", updated)
	}
}

func TestProductAdminHandler_Create_DynamicWithoutVariantPricesFailsClosed(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Create must not run when the own-price check is unwired")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", jsonBody(t, map[string]interface{}{
		"name": "Kit", "slug": "kit", "type": "bundle", "pricing_mode": "dynamic",
	}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
}

func TestProductAdminHandler_Update_DynamicWithoutVariantPricesFailsClosed(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Kit", Slug: "kit", Status: catalog.StatusDraft,
				Type: catalog.TypeBundle, PricingMode: catalog.PricingModeFixed,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run when the own-price check is unwired")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithTxStarter(adminFakeTxDBConn())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", jsonBody(t, map[string]interface{}{"pricing_mode": "dynamic"}))
	newAdminRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
}

func TestProductAdminHandler_Create_WithType(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "E-book",
		"slug": "e-book",
		"type": "virtual",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil || created.Type != catalog.TypeVirtual {
		t.Fatalf("type = %#v, want virtual", created)
	}
}

func TestProductAdminHandler_Create_WhitespaceTypeDefaultsSimple(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Plain",
		"slug": "plain",
		"type": "   ",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil || created.Type != catalog.TypeSimple {
		t.Fatalf("type = %#v, want simple (whitespace treated as omit)", created)
	}
}

func TestProductAdminHandler_Create_InvalidType(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Create must not be called for invalid type")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Bad",
		"slug": "bad",
		"type": "kit",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `invalid product type \"kit\"`) {
		t.Fatalf("body = %s, want invalid product type kit", rec.Body.String())
	}
}

func TestProductAdminHandler_Create_WithAttributes(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name":       "Widget",
		"slug":       "widget",
		"attributes": map[string]interface{}{"color": "blue"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created.Attributes["color"] != "blue" {
		t.Errorf("attributes[color] = %v, want blue", created.Attributes["color"])
	}
}

func TestProductAdminHandler_Create_MissingName(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Create_MissingSlug(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Create_InvalidBody(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte("not json")))
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Create_DuplicateSlug(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			return apperror.Conflict("product with this slug already exists")
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestProductAdminHandler_Create_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{
		"name": "Widget",
		"slug": "widget",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got == nil || got == "" {
		t.Errorf("resource_id = %v, want non-empty", got)
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

func TestProductAdminHandler_Create_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminProductRepo{}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte("not json")))
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
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

func TestProductAdminHandler_Create_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte(`{"name":"Widget","slug":"widget"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got == nil || got == "" {
		t.Errorf("resource_id = %v, want non-empty", got)
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

func TestProductAdminHandler_Create_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", bytes.NewReader([]byte("not json")))
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminCreateRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductCreate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductCreate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
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

// --- Update tests ---

func TestProductAdminHandler_Update_OK(t *testing.T) {
	existing := &catalog.Product{
		ID:              "p1",
		Name:            "Widget",
		Slug:            "widget",
		Description:     "old",
		Status:          catalog.StatusDraft,
		Type:            catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
		Attributes:      map[string]interface{}{},
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{
		"name":        "Updated Widget",
		"description": "new desc",
		"status":      "active",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated == nil {
		t.Fatal("product was not updated")
	}
	if updated.Name != "Updated Widget" {
		t.Errorf("name = %q, want 'Updated Widget'", updated.Name)
	}
	if updated.Description != "new desc" {
		t.Errorf("description = %q, want 'new desc'", updated.Description)
	}
	if updated.Status != catalog.StatusActive {
		t.Errorf("status = %q, want active", updated.Status)
	}
}

func TestProductAdminHandler_Update_Type(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "downloadable"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeDownloadable {
		t.Fatalf("type = %#v, want downloadable", updated)
	}
}

func TestProductAdminHandler_Update_TypeChangeBundleWithComponentsRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Kit", Slug: "kit", Status: catalog.StatusDraft, Type: catalog.TypeBundle,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{bundleCount: 2}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "simple"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "product has bundle components") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeUsedAsComponentRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Part", Slug: "part", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{componentCount: 1}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "grouped"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "used as a bundle component") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeUsesLockedGroupedType(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Set", Slug: "set", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		lockForUpdateFn: func(context.Context, string) (catalog.Type, error) {
			return catalog.TypeGrouped, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{groupCount: 2}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "simple"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "product has grouped members") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeIllicitGroupedRowsRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Set", Slug: "set", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		lockForUpdateFn: func(context.Context, string) (catalog.Type, error) {
			return catalog.TypeSimple, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{groupCount: 1}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "virtual"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "product has grouped members") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeIllicitBundleRowsRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Kit", Slug: "kit", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		lockForUpdateFn: func(context.Context, string) (catalog.Type, error) {
			return catalog.TypeSimple, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{bundleCount: 2}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "virtual"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "product has bundle components") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeUsesLockedType(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Part", Slug: "part", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		lockForUpdateFn: func(context.Context, string) (catalog.Type, error) {
			return catalog.TypeBundle, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{bundleCount: 2}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "grouped"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "product has bundle components") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeBundleWithZeroComponentsAllowed(t *testing.T) {
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Kit", Slug: "kit", Status: catalog.StatusDraft, Type: catalog.TypeBundle,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "simple"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeSimple {
		t.Fatalf("type = %#v, want simple", updated)
	}
}

func TestProductAdminHandler_Update_TypeChangeLeafToLeafWhileUsedAsComponentAllowed(t *testing.T) {
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Part", Slug: "part", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{componentCount: 1}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "virtual"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeVirtual {
		t.Fatalf("type = %#v, want virtual", updated)
	}
}

func TestProductAdminHandler_Update_TypeChangeWithoutBundleRepoFailsClosed(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run when the bundle repo is unwired")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "virtual"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeWithoutGroupedRepoFailsClosed(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run when the grouped repo is unwired")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "virtual"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeGroupedWithMembersRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Set", Slug: "set", Status: catalog.StatusDraft, Type: catalog.TypeGrouped,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{groupCount: 2}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "simple"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "product has grouped members") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeUsedAsMemberRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Part", Slug: "part", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not run")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{memberCount: 1}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "bundle"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "used as a grouped member") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_TypeChangeGroupedWithZeroMembersAllowed(t *testing.T) {
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Set", Slug: "set", Status: catalog.StatusDraft, Type: catalog.TypeGrouped,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "simple"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeSimple {
		t.Fatalf("type = %#v, want simple", updated)
	}
}

func TestProductAdminHandler_Update_TypeChangeLeafToLeafWhileUsedAsMemberAllowed(t *testing.T) {
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Part", Slug: "part", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{memberCount: 1}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "downloadable"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeDownloadable {
		t.Fatalf("type = %#v, want downloadable", updated)
	}
}

func TestProductAdminHandler_Update_OmitTypePreserves(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeVirtual,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{"name": "Renamed"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if updated == nil || updated.Type != catalog.TypeVirtual {
		t.Fatalf("type = %#v, want virtual preserved", updated)
	}
}

func TestProductAdminHandler_Update_EmptyTypeRejected(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w", Status: catalog.StatusDraft, Type: catalog.TypeSimple}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not be called for empty type")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{})

	body := jsonBody(t, map[string]interface{}{"type": "  "})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "type must not be empty") {
		t.Fatalf("body = %s, want type must not be empty", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_RequestInvalidType(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w", Status: catalog.StatusDraft, Type: catalog.TypeSimple}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error {
			t.Fatal("Update must not be called for invalid type")
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus()).
		WithBundleComponents(&mockBundleComponentGuard{}).
		WithGroupedMembers(&mockGroupedMemberGuard{}).
		WithTxStarter(adminFakeTxDBConn())

	body := jsonBody(t, map[string]interface{}{"type": "kit"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `invalid product type \"kit\"`) {
		t.Fatalf("body = %s, want invalid product type kit", rec.Body.String())
	}
}

func TestProductAdminHandler_Update_NotFound(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return nil, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/missing", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestProductAdminHandler_Update_InvalidStatus(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w", Type: catalog.TypeSimple}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{"status": "bogus"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_InvalidType(t *testing.T) {
	sink := &auditSink{}
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "W", Slug: "w", Status: catalog.StatusDraft, Type: catalog.Type("kit"),
			}, nil
		},
	}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"description": "x"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `invalid product type \"kit\"`) {
		t.Fatalf("body = %s, want invalid product type kit", rec.Body.String())
	}
	last := sink.Last(t)
	if last.context["result"] != "error" {
		t.Errorf("audit result = %v, want error", last.context["result"])
	}
	errMsg, _ := last.context["error"].(string)
	if !strings.Contains(errMsg, `"kit"`) {
		t.Errorf("audit error = %q, want quoted kit", errMsg)
	}
}

func TestProductAdminHandler_Update_EmptyName(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w", Type: catalog.TypeSimple}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	name := ""
	body := jsonBody(t, map[string]interface{}{"name": name})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_EmptySlug(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w", Type: catalog.TypeSimple}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	slug := ""
	body := jsonBody(t, map[string]interface{}{"slug": slug})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_InvalidBody(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "W", Slug: "w", Type: catalog.TypeSimple}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", bytes.NewReader([]byte("bad")))
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestProductAdminHandler_Update_PartialUpdate(t *testing.T) {
	existing := &catalog.Product{
		ID:              "p1",
		Name:            "Widget",
		Slug:            "widget",
		Description:     "desc",
		Status:          catalog.StatusDraft,
		Type:            catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
		Attributes:      map[string]interface{}{},
	}
	var updated *catalog.Product
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			cp := *existing
			return &cp, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			updated = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	// Only update description — name, slug, status stay the same.
	body := jsonBody(t, map[string]interface{}{"description": "new desc"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated.Name != "Widget" {
		t.Errorf("name = %q, want Widget (unchanged)", updated.Name)
	}
	if updated.Slug != "widget" {
		t.Errorf("slug = %q, want widget (unchanged)", updated.Slug)
	}
	if updated.Description != "new desc" {
		t.Errorf("description = %q, want 'new desc'", updated.Description)
	}
	if updated.Status != catalog.StatusDraft {
		t.Errorf("status = %q, want draft (unchanged)", updated.Status)
	}
}

func TestProductAdminHandler_Update_RepoError(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "W", Slug: "w", Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			return apperror.Internal("db down")
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	body := jsonBody(t, map[string]interface{}{"name": "X"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

func TestProductAdminHandler_Update_AuditIncludesScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"status": "active"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductUpdate)
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
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

func TestProductAdminHandler_Update_AuditFailureIncludesError(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple}, nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", bytes.NewReader([]byte("bad")))
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	req.Header.Set("X-Admin-Currency", "EUR")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductUpdate)
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
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

func TestProductAdminHandler_Update_AuditOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, p *catalog.Product) error {
			return nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"status": "active"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductUpdate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
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

func TestProductAdminHandler_Update_AuditFailureOmitsPartialScopeContext(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusDraft, Type: catalog.TypeSimple}, nil
		},
	}
	sink := &auditSink{}
	h := admin.NewProductAdminHandlerWithAuditor(repo, testAdminBus(), adminapp.NewAuditor(sink), logger.NewWithWriter(io.Discard, "info"))

	body := jsonBody(t, map[string]interface{}{"status": "invalid"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	req = testhelper.AdminRequest(req, "admin-1")
	req.Header.Set("X-Admin-Store-ID", "store-eu")
	req.Header.Set("X-Admin-Language", "en")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnprocessableEntity, rec.Body.String())
	}

	entry := sink.Last(t)
	if entry.event != "admin.action.failed" {
		t.Fatalf("event = %q, want %q", entry.event, "admin.action.failed")
	}
	if got := entry.context["action"]; got != adminapp.AuditProductUpdate {
		t.Errorf("action = %v, want %q", got, adminapp.AuditProductUpdate)
	}
	if got := entry.context["resource_type"]; got != "product" {
		t.Errorf("resource_type = %v, want %q", got, "product")
	}
	if got := entry.context["resource_id"]; got != "p1" {
		t.Errorf("resource_id = %v, want %q", got, "p1")
	}
	if got := entry.context["admin_id"]; got != "admin-1" {
		t.Errorf("admin_id = %v, want %q", got, "admin-1")
	}
	if got := entry.context["result"]; got != "error" {
		t.Errorf("result = %v, want %q", got, "error")
	}
	if got := entry.context["error"]; got == nil || got == "" {
		t.Errorf("error = %v, want non-empty", got)
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

// --- Event emission tests ---

func TestProductAdminHandler_Create_EmitsEvent(t *testing.T) {
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, _ *catalog.Product) error { return nil },
	}
	bus := testAdminBus()

	var captured event.Event
	bus.On(catalog.EventProductCreated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	h := admin.NewProductAdminHandler(repo, bus)
	body := jsonBody(t, map[string]interface{}{"name": "Widget", "slug": "widget"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if captured.Name != catalog.EventProductCreated {
		t.Fatalf("event name = %q, want %q", captured.Name, catalog.EventProductCreated)
	}
	data, ok := captured.Data.(catalog.ProductCreatedData)
	if !ok {
		t.Fatalf("event data type = %T, want ProductCreatedData", captured.Data)
	}
	if data.Name != "Widget" {
		t.Errorf("data.Name = %q, want Widget", data.Name)
	}
	if data.Slug != "widget" {
		t.Errorf("data.Slug = %q, want widget", data.Slug)
	}
}

func TestProductAdminHandler_Update_EmitsEvent(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{
				ID: id, Name: "Old", Slug: "old", Status: catalog.StatusDraft, Type: catalog.TypeSimple,
				VisibilityModes: catalog.DefaultVisibilityAxes(),
			}, nil
		},
		updateFn: func(_ context.Context, _ *catalog.Product) error { return nil },
	}
	bus := testAdminBus()

	var captured event.Event
	bus.On(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	h := admin.NewProductAdminHandler(repo, bus)
	body := jsonBody(t, map[string]interface{}{"name": "New Name"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/admin/products/p1", body)
	newAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Name != catalog.EventProductUpdated {
		t.Fatalf("event name = %q, want %q", captured.Name, catalog.EventProductUpdated)
	}
	data, ok := captured.Data.(catalog.ProductUpdatedData)
	if !ok {
		t.Fatalf("event data type = %T, want ProductUpdatedData", captured.Data)
	}
	if data.Name != "New Name" {
		t.Errorf("data.Name = %q, want 'New Name'", data.Name)
	}
}

// ── product permission guard tests ─────────────────────────────────────

func createProductBody(t *testing.T) *bytes.Reader {
	t.Helper()
	return jsonBody(t, map[string]interface{}{"name": "Widget", "slug": "widget"})
}

func newGuardedAdminRouter(h *admin.ProductAdminHandler) *http.ServeMux {
	requireProductsRead := admin.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := admin.RequirePermission(rbac.ProductsWrite)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products", requireProductsRead(h.List()))
	mux.Handle("POST /api/v1/admin/products", requireProductsWrite(h.Create()))
	mux.Handle("PUT /api/v1/admin/products/{id}", requireProductsWrite(h.Update()))
	return mux
}

func TestAdminGuard_CustomerForbidden(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req = testhelper.CustomerRequest(req, "cust-1")
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminGuard_GuestUnauthorized(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAdminGuard_SupportListAllowed(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset != 0 {
				t.Errorf("offset = %d, want 0", filter.Offset)
			}
			if filter.Limit != 20 {
				t.Errorf("limit = %d, want 20", filter.Limit)
			}
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminGuardWithAudit_ProductGetUnauthorized(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAdminGuardWithAudit_ProductGetForbidden(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	req = testhelper.CustomerRequest(req, "cust-1")
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminGuardWithAudit_ProductGetAllowed(t *testing.T) {
	repo := &mockAdminProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: id, Name: "Widget", Slug: "widget", Status: catalog.StatusActive, Type: catalog.TypeSimple}, nil
		},
		listCategoryIDsByProductFn: func(_ context.Context, productID string) ([]string, error) {
			return []string{"cat-1"}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products/p1", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	newAdminRouterWithAudit(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminGuard_EditorAllowed(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	newGuardedAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product should have been created")
	}
}

// ── integration test: AuthMiddleware → RequirePermission(...) ──────────

// stubAdminTokenParser parses test tokens of the form "test-token:<userID>:<role>".
type stubAdminTokenParser struct{}

func (p *stubAdminTokenParser) Parse(_ context.Context, token string) (identity.Identity, error) {
	parts := strings.SplitN(token, ":", 3)
	if len(parts) != 3 || parts[0] != "test-token" {
		return identity.Identity{}, errors.New("invalid test token")
	}
	role := identity.Role(parts[2])
	if !role.IsValid() {
		return identity.Identity{}, errors.New("invalid role: " + parts[2])
	}
	return identity.NewIdentity(parts[1], role)
}

func newIntegrationAdminRouter(h *admin.ProductAdminHandler) http.Handler {
	requireProductsRead := admin.RequirePermission(rbac.ProductsRead)
	requireProductsWrite := admin.RequirePermission(rbac.ProductsWrite)
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/products", requireProductsRead(h.List()))
	mux.Handle("POST /api/v1/admin/products", requireProductsWrite(h.Create()))

	authMW := storefront.AuthMiddleware(&stubAdminTokenParser{})
	return authMW(mux)
}

func TestAdminGuard_Integration_NoToken(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestAdminGuard_Integration_CustomerToken(t *testing.T) {
	repo := &mockAdminProductRepo{}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req.Header.Set("Authorization", "Bearer test-token:cust-1:customer")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestAdminGuard_Integration_SupportTokenListAllowed(t *testing.T) {
	repo := &mockAdminProductRepo{
		listFn: func(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
			if filter.Offset != 0 {
				t.Errorf("offset = %d, want 0", filter.Offset)
			}
			if filter.Limit != 20 {
				t.Errorf("limit = %d, want 20", filter.Limit)
			}
			return []catalog.Product{{ID: "p1", Name: "Widget", Slug: "widget", Status: catalog.StatusActive}}, nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/admin/products", nil)
	req.Header.Set("Authorization", "Bearer test-token:support-1:support")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAdminGuard_Integration_EditorToken(t *testing.T) {
	var created *catalog.Product
	repo := &mockAdminProductRepo{
		createFn: func(_ context.Context, p *catalog.Product) error {
			created = p
			return nil
		},
	}
	h := admin.NewProductAdminHandler(repo, testAdminBus())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/admin/products", createProductBody(t))
	req.Header.Set("Authorization", "Bearer test-token:editor-1:editor")
	newIntegrationAdminRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}
	if created == nil {
		t.Fatal("product should have been created")
	}
}
