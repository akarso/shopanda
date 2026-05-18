package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/event"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// --- mock category repository ---

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

// --- mock product repository (category-scoped) ---

type mockCatProductRepo struct {
	findByIDFn         func(ctx context.Context, id string) (*catalog.Product, error)
	findBySlugFn       func(ctx context.Context, slug string) (*catalog.Product, error)
	listFn             func(ctx context.Context, offset, limit int) ([]catalog.Product, error)
	findByCategoryIDFn func(ctx context.Context, categoryID string, offset, limit int) ([]catalog.Product, error)
	createFn           func(ctx context.Context, p *catalog.Product) error
	updateFn           func(ctx context.Context, p *catalog.Product) error
}

type mockProductCategoryAssignmentRepo struct {
	assignFn func(ctx context.Context, productID, categoryID string) error
	removeFn func(ctx context.Context, productID, categoryID string) error
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

// --- router helper ---

func newCategoryRouter(h *shophttp.CategoryHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/categories", h.Tree())
	mux.HandleFunc("GET /api/v1/categories/{id}", h.Get())
	mux.HandleFunc("GET /api/v1/categories/{id}/products", h.Products())
	return mux
}

func newAdminCategoryRouter(h *shophttp.CategoryHandler) *http.ServeMux {
	requireCategoriesRead := shophttp.RequirePermission(rbac.CategoriesRead)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/categories", withAdminContext(requireCategoriesRead(h.Tree())))
	return mux
}

func newAdminCategoryCRUDRouter(read *shophttp.CategoryHandler, admin *shophttp.CategoryAdminHandler) *http.ServeMux {
	requireCategoriesRead := shophttp.RequirePermission(rbac.CategoriesRead)
	requireCategoriesWrite := shophttp.RequirePermission(rbac.CategoriesWrite)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/categories", withAdminContext(requireCategoriesRead(read.Tree())))
	mux.Handle("GET /api/v1/admin/categories/{id}", withAdminContext(requireCategoriesRead(read.Get())))
	mux.Handle("POST /api/v1/admin/categories", withAdminContext(requireCategoriesWrite(admin.Create())))
	mux.Handle("PUT /api/v1/admin/categories/{id}", withAdminContext(requireCategoriesWrite(admin.Update())))
	mux.Handle("DELETE /api/v1/admin/categories/{id}", withAdminContext(requireCategoriesWrite(admin.Delete())))
	return mux
}

func newAdminCategoryAssignmentRouter(read *shophttp.CategoryHandler, assignment *shophttp.CategoryProductAssignmentAdminHandler) *http.ServeMux {
	requireCategoriesRead := shophttp.RequirePermission(rbac.CategoriesRead)
	requireCategoriesWrite := shophttp.RequirePermission(rbac.CategoriesWrite)
	withAdminContext := shophttp.AdminContextMiddleware()
	mux := http.NewServeMux()
	mux.Handle("GET /api/v1/admin/categories/{id}/products", withAdminContext(requireCategoriesRead(read.Products())))
	mux.Handle("POST /api/v1/admin/categories/{id}/products/{productId}", withAdminContext(requireCategoriesWrite(assignment.Assign())))
	mux.Handle("DELETE /api/v1/admin/categories/{id}/products/{productId}", withAdminContext(requireCategoriesWrite(assignment.Unassign())))
	return mux
}

func categoryBus() *event.Bus {
	return event.NewBus(logger.NewWithWriter(io.Discard, "error"))
}

// --- tests ---

func TestCategoryHandler_Tree_OK(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			parentID := "cat-1"
			return []catalog.Category{
				{ID: "cat-1", Name: "Electronics", Slug: "electronics", Position: 1},
				{ID: "cat-2", ParentID: &parentID, Name: "Phones", Slug: "phones", Position: 1},
			}, nil
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories")
	if err != nil {
		t.Fatalf("GET categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Categories []map[string]interface{} `json:"categories"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}

	tree := envelope.Data.Categories
	if len(tree) != 1 {
		t.Fatalf("root count = %d, want 1", len(tree))
	}
	if tree[0]["name"] != "Electronics" {
		t.Errorf("root name = %v, want Electronics", tree[0]["name"])
	}
	children, ok := tree[0]["children"].([]interface{})
	if !ok || len(children) != 1 {
		t.Fatalf("children count = %v, want 1", tree[0]["children"])
	}
}

func TestCategoryHandler_Tree_RepoError(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return nil, errors.New("db down")
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories")
	if err != nil {
		t.Fatalf("GET categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestCategoryHandler_Get_OK(t *testing.T) {
	cat := &catalog.Category{ID: "cat-1", Name: "Books", Slug: "books"}
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return cat, nil
			}
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/cat-1")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Category json.RawMessage `json:"category"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Category) == 0 {
		t.Fatal("response missing 'category' in data")
	}
}

func TestCategoryHandler_Get_NotFound(t *testing.T) {
	cats := &mockCategoryRepo{}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/missing-id")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCategoryHandler_Products_OK(t *testing.T) {
	cat := &catalog.Category{ID: "cat-1", Name: "Books", Slug: "books"}
	cats := &mockCategoryRepo{
		findByIDFn: func(_ context.Context, id string) (*catalog.Category, error) {
			if id == "cat-1" {
				return cat, nil
			}
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{
		findByCategoryIDFn: func(_ context.Context, catID string, offset, limit int) ([]catalog.Product, error) {
			if catID != "cat-1" {
				t.Fatalf("expected catID %q, got %q", "cat-1", catID)
			}
			return []catalog.Product{
				{ID: "p-1", Name: "Go Book", Slug: "go-book"},
			}, nil
		},
	}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/cat-1/products")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Products json.RawMessage `json:"products"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Products) == 0 {
		t.Fatal("response missing 'products' in data")
	}
}

func TestCategoryHandler_Products_CategoryNotFound(t *testing.T) {
	cats := &mockCategoryRepo{}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories/missing-id/products")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCategoryHandler_Tree_Empty(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return nil, nil
		},
	}
	prods := &mockCatProductRepo{}
	h := shophttp.NewCategoryHandler(cats, prods)
	srv := httptest.NewServer(newCategoryRouter(h))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/categories")
	if err != nil {
		t.Fatalf("GET categories: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var envelope struct {
		Data struct {
			Categories []json.RawMessage `json:"categories"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Categories) != 0 {
		t.Fatalf("categories count = %d, want 0", len(envelope.Data.Categories))
	}
}

func TestCategoryHandler_AdminTree_OK(t *testing.T) {
	cats := &mockCategoryRepo{
		findAllFn: func(_ context.Context) ([]catalog.Category, error) {
			return []catalog.Category{
				{ID: "cat-1", Name: "Electronics", Slug: "electronics", Position: 1},
			}, nil
		},
	}
	h := shophttp.NewCategoryHandler(cats, &mockCatProductRepo{})
	mux := newAdminCategoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/categories", nil)
	req = testhelper.AuthenticatedRequest(req, "editor-1", identity.RoleEditor)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var envelope struct {
		Data struct {
			Categories []json.RawMessage `json:"categories"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(envelope.Data.Categories) != 1 {
		t.Fatalf("categories count = %d, want 1", len(envelope.Data.Categories))
	}
}

func TestCategoryHandler_AdminTree_Forbidden(t *testing.T) {
	h := shophttp.NewCategoryHandler(&mockCategoryRepo{}, &mockCatProductRepo{})
	mux := newAdminCategoryRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/categories", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(repo, &mockCatProductRepo{})
	admin := shophttp.NewCategoryAdminHandler(repo, categoryBus())
	mux := newAdminCategoryCRUDRouter(read, admin)

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
	read := shophttp.NewCategoryHandler(cats, prods)
	h := shophttp.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
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
	read := shophttp.NewCategoryHandler(cats, prods)
	h := shophttp.NewCategoryProductAssignmentAdminHandler(cats, prods, assignments)
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
}

func TestCategoryProductAssignmentAdminHandler_Assign_Forbidden(t *testing.T) {
	cats := &mockCategoryRepo{}
	prods := &mockCatProductRepo{}
	read := shophttp.NewCategoryHandler(cats, prods)
	h := shophttp.NewCategoryProductAssignmentAdminHandler(cats, prods, &mockProductCategoryAssignmentRepo{})
	mux := newAdminCategoryAssignmentRouter(read, h)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/categories/cat-1/products/prod-1", nil)
	req = testhelper.AuthenticatedRequest(req, "support-1", identity.RoleSupport)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func strPtr(v string) *string {
	return &v
}
