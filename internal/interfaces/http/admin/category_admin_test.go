package admin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	storefront "github.com/akarso/shopanda/internal/interfaces/http/storefront"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// mockCategoryRepo/mockCatProductRepo duplicate the storefront category_test.go
// mocks — unexported, so they can't be shared across the http_test/admin_test
// package boundary created by the admin package split.

type mockCategoryRepo struct {
	findByIDFn     func(ctx context.Context, id string) (*catalog.Category, error)
	findBySlugFn   func(ctx context.Context, slug string) (*catalog.Category, error)
	findByParentFn func(ctx context.Context, parentID *string) ([]catalog.Category, error)
	findAllFn      func(ctx context.Context) ([]catalog.Category, error)
	createFn       func(ctx context.Context, c *catalog.Category) error
	updateFn       func(ctx context.Context, c *catalog.Category) error
	deleteFn       func(ctx context.Context, id string) error
}

func (m *mockCategoryRepo) FindByID(ctx context.Context, id string) (*catalog.Category, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Category, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindByParentID(ctx context.Context, parentID *string) ([]catalog.Category, error) {
	if m.findByParentFn != nil {
		return m.findByParentFn(ctx, parentID)
	}
	return nil, nil
}

func (m *mockCategoryRepo) FindAll(ctx context.Context) ([]catalog.Category, error) {
	if m.findAllFn != nil {
		return m.findAllFn(ctx)
	}
	return nil, nil
}

func (m *mockCategoryRepo) Create(ctx context.Context, c *catalog.Category) error {
	if m.createFn != nil {
		return m.createFn(ctx, c)
	}
	return nil
}

func (m *mockCategoryRepo) Update(ctx context.Context, c *catalog.Category) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, c)
	}
	return nil
}

func (m *mockCategoryRepo) Delete(ctx context.Context, id string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

type mockCatProductRepo struct {
	findByIDFn         func(ctx context.Context, id string) (*catalog.Product, error)
	findBySlugFn       func(ctx context.Context, slug string) (*catalog.Product, error)
	listFn             func(ctx context.Context, offset, limit int) ([]catalog.Product, error)
	findByCategoryIDFn func(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error)
	createFn           func(ctx context.Context, p *catalog.Product) error
	updateFn           func(ctx context.Context, p *catalog.Product) error
}

type mockProductCategoryAssignmentRepo struct {
	assignFn                   func(ctx context.Context, productID, categoryID string) error
	removeFn                   func(ctx context.Context, productID, categoryID string) error
	listCategoryIDsByProductFn func(ctx context.Context, productID string) ([]string, error)
}

func (m *mockCatProductRepo) FindByID(ctx context.Context, id string) (*catalog.Product, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockCatProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}

func (m *mockCatProductRepo) List(ctx context.Context, offset, limit int) ([]catalog.Product, error) {
	if m.listFn != nil {
		return m.listFn(ctx, offset, limit)
	}
	return nil, nil
}

func (m *mockCatProductRepo) FindByCategoryID(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error) {
	if m.findByCategoryIDFn != nil {
		return m.findByCategoryIDFn(ctx, categoryID, offset, limit)
	}
	return nil, nil
}

func (m *mockCatProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	return nil
}

func (m *mockCatProductRepo) Update(ctx context.Context, p *catalog.Product) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, p)
	}
	return nil
}

func (m *mockProductCategoryAssignmentRepo) AssignCategory(ctx context.Context, productID, categoryID string) error {
	if m.assignFn != nil {
		return m.assignFn(ctx, productID, categoryID)
	}
	return nil
}

func (m *mockProductCategoryAssignmentRepo) RemoveCategory(ctx context.Context, productID, categoryID string) error {
	if m.removeFn != nil {
		return m.removeFn(ctx, productID, categoryID)
	}
	return nil
}

func (m *mockProductCategoryAssignmentRepo) ListCategoryIDsByProduct(ctx context.Context, productID string) ([]string, error) {
	if m.listCategoryIDsByProductFn != nil {
		return m.listCategoryIDsByProductFn(ctx, productID)
	}
	return nil, nil
}

// --- router helpers ---

func newAdminCategoryCRUDRouter(read *storefront.CategoryHandler, adm *admin.CategoryAdminHandler) *http.ServeMux {
	requireCategoriesRead := admin.RequirePermission(rbac.CategoriesRead)
	requireCategoriesWrite := admin.RequirePermission(rbac.CategoriesWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/categories", withAdminContext(requireCategoriesRead(read.Tree())))
	mux.Handle("GET /api/v1/admin/categories/{id}", withAdminContext(requireCategoriesRead(read.Get())))
	mux.Handle("POST /api/v1/admin/categories", withAdminContext(requireCategoriesWrite(adm.Create())))
	mux.Handle("PUT /api/v1/admin/categories/{id}", withAdminContext(requireCategoriesWrite(adm.Update())))
	mux.Handle("DELETE /api/v1/admin/categories/{id}", withAdminContext(requireCategoriesWrite(adm.Delete())))
	return mux
}

func newAdminCategoryAssignmentRouter(read *storefront.CategoryHandler, assignment *admin.CategoryProductAssignmentAdminHandler) *http.ServeMux {
	requireCategoriesRead := admin.RequirePermission(rbac.CategoriesRead)
	requireCategoriesWrite := admin.RequirePermission(rbac.CategoriesWrite)
	withAdminContext := admin.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/categories/{id}/products", withAdminContext(requireCategoriesRead(read.Products())))
	mux.Handle("POST /api/v1/admin/categories/{id}/products/{productId}", withAdminContext(requireCategoriesWrite(assignment.Assign())))
	mux.Handle("DELETE /api/v1/admin/categories/{id}/products/{productId}", withAdminContext(requireCategoriesWrite(assignment.Unassign())))
	return mux
}

func categoryBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
}

func strPtr(v string) *string {
	return &v
}

// --- tests ---

func TestCategoryAdminHandler_Create_OK(t *testing.T) {
	var created *catalog.Category
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "root-1" {
				return &catalog.Category{ID: "root-1", Name: "Root", Slug: "root"}, nil
			}
			return nil, nil
		},
		createFn: func(_ context.Context, c *catalog.Category) error {
			clone := *c
			created = &clone
			return nil
		},
	}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	body := bytes.NewBufferString(`{"name":"Accessories","slug":"accessories","parent_id":"root-1","position":3,"meta":{"nav_label":"Accessories"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories", body)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if created == nil {
		t.Fatal("expected category to be created")
	}
	if created.ParentID == nil || *created.ParentID != "root-1" {
		t.Fatalf("parent_id = %v, want root-1", created.ParentID)
	}
	if created.Position != 3 {
		t.Fatalf("position = %d, want 3", created.Position)
	}
	if created.Meta["nav_label"] != "Accessories" {
		t.Fatalf("meta.nav_label = %v, want Accessories", created.Meta["nav_label"])
	}
}

func TestCategoryAdminHandler_Update_OK(t *testing.T) {
	category := &catalog.Category{ID: "cat-1", ParentID: strPtr("root-1"), Name: "Old", Slug: "old", Position: 1, Meta: map[string]interface{}{"nav_label": "Old"}}
	var updated *catalog.Category
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			switch id {
			case "cat-1":
				return category, nil
			case "root-2":
				return &catalog.Category{ID: "root-2", Name: "Root Two", Slug: "root-two"}, nil
			default:
				return nil, nil
			}
		},
		updateFn: func(_ context.Context, c *catalog.Category) error {
			clone := *c
			updated = &clone
			return nil
		},
	}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	body := bytes.NewBufferString(`{"name":"New","slug":"new","parent_id":"root-2","position":7,"meta":{"nav_label":"New"}}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/categories/cat-1", body)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if updated == nil {
		t.Fatal("expected category to be updated")
	}
	if updated.Name != "New" || updated.Slug != "new" {
		t.Fatalf("updated category = %+v", updated)
	}
	if updated.ParentID == nil || *updated.ParentID != "root-2" {
		t.Fatalf("parent_id = %v, want root-2", updated.ParentID)
	}
	if updated.Position != 7 {
		t.Fatalf("position = %d, want 7", updated.Position)
	}
}

func TestCategoryAdminHandler_Update_RejectsCycle(t *testing.T) {
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			switch id {
			case "cat-1":
				return &catalog.Category{ID: "cat-1", ParentID: nil, Name: "Root", Slug: "root"}, nil
			case "child-1":
				return &catalog.Category{ID: "child-1", ParentID: strPtr("cat-1"), Name: "Child", Slug: "child"}, nil
			default:
				return nil, nil
			}
		},
	}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	body := bytes.NewBufferString(`{"parent_id":"child-1"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/categories/cat-1", body)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
}

func TestCategoryAdminHandler_Update_RejectsRepeatedAncestor(t *testing.T) {
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			switch id {
			case "cat-1":
				return &catalog.Category{ID: "cat-1", Name: "Root", Slug: "root"}, nil
			case "child-1":
				return &catalog.Category{ID: "child-1", ParentID: strPtr("loop-1"), Name: "Child", Slug: "child"}, nil
			case "loop-1":
				return &catalog.Category{ID: "loop-1", ParentID: strPtr("loop-2"), Name: "Loop 1", Slug: "loop-1"}, nil
			case "loop-2":
				return &catalog.Category{ID: "loop-2", ParentID: strPtr("loop-1"), Name: "Loop 2", Slug: "loop-2"}, nil
			default:
				return nil, nil
			}
		},
	}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	body := bytes.NewBufferString(`{"parent_id":"child-1"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/categories/cat-1", body)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("invalid parent chain: cycle detected at ID loop-1")) {
		t.Fatalf("body = %s, want cycle-detected message", rec.Body.String())
	}
}

func TestCategoryAdminHandler_Delete_OK(t *testing.T) {
	deletedID := ""
	repo := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
			}
			return nil, nil
		},
		deleteFn: func(_ context.Context, id string) error {
			deletedID = id
			return nil
		},
	}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/categories/cat-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if deletedID != "cat-1" {
		t.Fatalf("deleted id = %q, want %q", deletedID, "cat-1")
	}
}

func TestCategoryAdminHandler_Create_Forbidden(t *testing.T) {
	repo := &mockCategoryRepo{}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	body := bytes.NewBufferString(`{"name":"Accessories","slug":"accessories"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories", body)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCategoryAdminHandler_Update_Forbidden(t *testing.T) {
	repo := &mockCategoryRepo{}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	body := bytes.NewBufferString(`{"name":"Accessories","slug":"accessories"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/categories/1", body)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCategoryAdminHandler_Delete_Forbidden(t *testing.T) {
	repo := &mockCategoryRepo{}
	read := storefront.NewCategoryHandler(repo, &mockCatProductRepo{})
	adm := admin.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, adm)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/categories/1", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestCategoryProductAssignmentAdminHandler_Assign_OK(t *testing.T) {
	assigned := false
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
			}
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == "prod-1" {
				return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat"}, nil
			}
			return nil, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		assignFn: func(_ context.Context, productID, categoryID string) error {
			if productID != "prod-1" || categoryID != "cat-1" {
				t.Fatalf("assign(%q, %q), want (%q, %q)", productID, categoryID, "prod-1", "cat-1")
			}
			assigned = true
			return nil
		},
	}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
	mux := newAdminCategoryAssignmentRouter(read, h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !assigned {
		t.Fatal("expected assignment repo to be called")
	}

	var assignBody struct {
		Data struct {
			Assigned bool `json:"assigned"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &assignBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !assignBody.Data.Assigned {
		t.Fatalf("assigned = %v, want true", assignBody.Data.Assigned)
	}
}

// TestCategoryProductAssignmentAdminHandler_Assign_EmitsProductUpdatedEvent
// pins PR-1036's wiring: a successful Assign must publish
// catalog.EventProductUpdated (reused, not a new event — see
// PR-1036.md's "Design decisions") so the search index's on-save
// subscriber refreshes the product's indexed category membership. It must
// carry the real Name/Slug/Status, not just ProductID — rewrite.Subscriber
// also listens to this event synchronously and builds a URL rewrite
// straight from data.Slug with no fallback lookup; an empty Slug would
// register "/" itself as this product's rewrite (or hit a path conflict),
// aborting the publish before the async cache/search handlers even ran.
func TestCategoryProductAssignmentAdminHandler_Assign_EmitsProductUpdatedEvent(t *testing.T) {
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat", Status: catalog.StatusActive}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		assignFn: func(_ context.Context, productID, categoryID string) error { return nil },
	}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "error"))
	h.SetBus(bus)

	var captured event.Event
	bus.On(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	mux := newAdminCategoryAssignmentRouter(read, h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

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
	if data.ProductID != "prod-1" || data.Name != "Hat" || data.Slug != "hat" || data.Status != catalog.StatusActive {
		t.Fatalf("data = %+v, want product_id=prod-1 name=Hat slug=hat status=active", data)
	}
}

// TestCategoryProductAssignmentAdminHandler_Unassign_EmitsProductUpdatedEvent
// is Assign's counterpart for Unassign — same event, same payload
// requirement (real Slug, not empty).
func TestCategoryProductAssignmentAdminHandler_Unassign_EmitsProductUpdatedEvent(t *testing.T) {
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat", Status: catalog.StatusActive}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		removeFn: func(_ context.Context, productID, categoryID string) error { return nil },
	}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "error"))
	h.SetBus(bus)

	var captured event.Event
	bus.On(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	mux := newAdminCategoryAssignmentRouter(read, h)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

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
	if data.ProductID != "prod-1" || data.Name != "Hat" || data.Slug != "hat" || data.Status != catalog.StatusActive {
		t.Fatalf("data = %+v, want product_id=prod-1 name=Hat slug=hat status=active", data)
	}
}

// TestCategoryProductAssignmentAdminHandler_Assign_EmitsCategoryUpdatedEvent
// pins the CR fix for PR-1037: without also publishing
// catalog.EventCategoryUpdated on assignment change, the category's
// indexed ProductCount only ever refreshed on the category's own next
// create/update, never on a membership change made through this handler
// — the primary real-world workflow for assigning existing products to
// categories. This asserts the category-side event fires with the
// category's own (unchanged) Name/Slug and an empty OldSlug, alongside
// the pre-existing product-side event.
func TestCategoryProductAssignmentAdminHandler_Assign_EmitsCategoryUpdatedEvent(t *testing.T) {
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat", Status: catalog.StatusActive}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		assignFn: func(_ context.Context, productID, categoryID string) error { return nil },
	}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "error"))
	h.SetBus(bus)

	var captured event.Event
	bus.On(catalog.EventCategoryUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	mux := newAdminCategoryAssignmentRouter(read, h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Name != catalog.EventCategoryUpdated {
		t.Fatalf("event name = %q, want %q (ProductCount would never refresh on assignment change without this)", captured.Name, catalog.EventCategoryUpdated)
	}
	data, ok := captured.Data.(catalog.CategoryUpdatedData)
	if !ok {
		t.Fatalf("event data type = %T, want CategoryUpdatedData", captured.Data)
	}
	if data.CategoryID != "cat-1" || data.Name != "Accessories" || data.Slug != "accessories" {
		t.Fatalf("data = %+v, want category_id=cat-1 name=Accessories slug=accessories", data)
	}
	if data.OldSlug != "" {
		t.Errorf("OldSlug = %q, want empty (assignment doesn't change the category's own slug)", data.OldSlug)
	}
}

// TestCategoryProductAssignmentAdminHandler_Unassign_EmitsCategoryUpdatedEvent
// is Assign's counterpart for Unassign — see
// TestCategoryProductAssignmentAdminHandler_Assign_EmitsCategoryUpdatedEvent.
func TestCategoryProductAssignmentAdminHandler_Unassign_EmitsCategoryUpdatedEvent(t *testing.T) {
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat", Status: catalog.StatusActive}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		removeFn: func(_ context.Context, productID, categoryID string) error { return nil },
	}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "error"))
	h.SetBus(bus)

	var captured event.Event
	bus.On(catalog.EventCategoryUpdated, func(_ context.Context, evt event.Event) error {
		captured = evt
		return nil
	})

	mux := newAdminCategoryAssignmentRouter(read, h)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if captured.Name != catalog.EventCategoryUpdated {
		t.Fatalf("event name = %q, want %q", captured.Name, catalog.EventCategoryUpdated)
	}
	data, ok := captured.Data.(catalog.CategoryUpdatedData)
	if !ok {
		t.Fatalf("event data type = %T, want CategoryUpdatedData", captured.Data)
	}
	if data.CategoryID != "cat-1" || data.Name != "Accessories" || data.Slug != "accessories" {
		t.Fatalf("data = %+v, want category_id=cat-1 name=Accessories slug=accessories", data)
	}
}

// TestCategoryProductAssignmentAdminHandler_Assign_PublishFailure_StillSucceedsButAudited
// pins two fixes together: event.Bus.Publish aborts before dispatching any
// async handler (cache invalidation, search reindex) if a synchronous
// handler on the same event — rewrite.Subscriber, here simulated directly
// on the bus — returns an error (a genuine path conflict, or a repository
// failure; not just the empty-Slug case Round 2 already fixed). The
// assignment write itself already succeeded by that point, so Assign must
// still report success (this codebase's own convention: a side effect's
// own failure never fails the response — see product_admin.go's own
// Publish call). Two things must both be true when the sync handler
// fails: (1) the async handlers still actually run — via
// publishAssignmentChanged's PublishAsync fallback (Round 4) — so the
// index/cache don't go stale over an unrelated rewrite problem, and (2)
// the sync failure is still recorded in a separate audit entry (Round 3),
// so it's visible to whoever reviews the audit log even though nothing
// about it blocked the request or the index update.
func TestCategoryProductAssignmentAdminHandler_Assign_PublishFailure_StillSucceedsButAudited(t *testing.T) {
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat", Status: catalog.StatusActive}, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		assignFn: func(_ context.Context, productID, categoryID string) error { return nil },
	}
	read := storefront.NewCategoryHandler(cats, prods)

	sink := &auditSink{}
	h := admin.NewCategoryProductAssignmentAdminHandlerWithAuditor(cats, prods, assignments, adminapp.NewAuditor(sink))
	bus := event.NewBus(logger.NewWithWriter(io.Discard, "error"))
	// Simulates rewrite.Subscriber (or any other synchronous handler on
	// this event) failing — e.g. a real path conflict — independent of
	// whether the payload itself is correct.
	bus.On(catalog.EventProductUpdated, func(context.Context, event.Event) error {
		return fmt.Errorf("rewrite: path %q already claimed by page/some-other-page", "/hat")
	})
	// Stands in for cache invalidation / the search-index subscriber: this
	// must still fire even though the sync handler above fails.
	asyncRan := make(chan struct{})
	bus.OnAsync(catalog.EventProductUpdated, func(_ context.Context, evt event.Event) error {
		data, ok := evt.Data.(catalog.ProductUpdatedData)
		if !ok || data.ProductID != "prod-1" {
			t.Errorf("async handler got unexpected event data: %+v", evt.Data)
		}
		close(asyncRan)
		return nil
	})
	h.SetBus(bus)

	mux := newAdminCategoryAssignmentRouter(read, h)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Data struct {
			Assigned bool `json:"assigned"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.Data.Assigned {
		t.Fatal("expected assigned=true — the assignment itself succeeded, a downstream event-publish failure must not flip this")
	}

	select {
	case <-asyncRan:
		// success — cache invalidation / search reindex still ran despite
		// the synchronous rewrite handler's failure.
	case <-time.After(2 * time.Second):
		t.Fatal("async handler (cache invalidation / search reindex stand-in) did not run — the sync handler's failure must not leave it stale")
	}

	if len(sink.records) != 2 {
		t.Fatalf("audit records = %d, want 2 (assignment success + publish failure); records=%+v", len(sink.records), sink.records)
	}
	last := sink.Last(t)
	if last.event != "admin.action.failed" {
		t.Fatalf("last audit event = %q, want admin.action.failed", last.event)
	}
	if last.context["result"] != "error" {
		t.Errorf("last audit result = %v, want error", last.context["result"])
	}
	errMsg, _ := last.context["error"].(string)
	if errMsg == "" {
		t.Error("expected the last audit record to carry a non-empty error message about the publish failure")
	}
}

func TestCategoryProductAssignmentAdminHandler_Unassign_OK(t *testing.T) {
	removed := false
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return &catalog.Category{ID: "cat-1", Name: "Accessories", Slug: "accessories"}, nil
			}
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Product, error) {
			if id == "prod-1" {
				return &catalog.Product{ID: "prod-1", Name: "Hat", Slug: "hat"}, nil
			}
			return nil, nil
		},
	}
	assignments := &mockProductCategoryAssignmentRepo{
		removeFn: func(_ context.Context, productID, categoryID string) error {
			if productID != "prod-1" || categoryID != "cat-1" {
				t.Fatalf("remove(%q, %q), want (%q, %q)", productID, categoryID, "prod-1", "cat-1")
			}
			removed = true
			return nil
		},
	}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
	mux := newAdminCategoryAssignmentRouter(read, h)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !removed {
		t.Fatal("expected assignment repo to be called")
	}

	var unassignBody struct {
		Data struct {
			Assigned bool `json:"assigned"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &unassignBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if unassignBody.Data.Assigned {
		t.Fatalf("assigned = %v, want false", unassignBody.Data.Assigned)
	}
}

func TestCategoryProductAssignmentAdminHandler_Assign_Forbidden(t *testing.T) {
	cats := &mockCategoryRepo{}
	prods := &mockCatProductRepo{}
	read := storefront.NewCategoryHandler(cats, prods)
	h := admin.NewCategoryProductAssignmentAdminHandler(cats, prods, &mockProductCategoryAssignmentRepo{})
	mux := newAdminCategoryAssignmentRouter(read, h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
