package admin_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	adminapp "github.com/akarso/shopanda/internal/application/admin"
	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/identity"
	"github.com/akarso/shopanda/internal/domain/rbac"
	"github.com/akarso/shopanda/internal/interfaces/http/admin"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/auth/testhelper"
	"github.com/akarso/shopanda/internal/platform/logger"
)

func init() {
	sql.Register("shopanda-linked-elig-admin-tx", linkedEligAdminTxDriver{})
}

type linkedEligAdminTxDriver struct{}

func (linkedEligAdminTxDriver) Open(string) (driver.Conn, error) { return linkedEligAdminTxConn{}, nil }

type linkedEligAdminTxConn struct{}

func (linkedEligAdminTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (linkedEligAdminTxConn) Close() error                        { return nil }
func (linkedEligAdminTxConn) Begin() (driver.Tx, error)           { return linkedEligAdminTx{}, nil }

type linkedEligAdminTx struct{}

func (linkedEligAdminTx) Commit() error   { return nil }
func (linkedEligAdminTx) Rollback() error { return nil }

var (
	linkedEligAdminTxOnce sync.Once
	linkedEligAdminTxDB   *sql.DB
)

func linkedEligAdminTxStarter() *sql.DB {
	linkedEligAdminTxOnce.Do(func() {
		db, err := sql.Open("shopanda-linked-elig-admin-tx", "")
		if err != nil {
			panic(err)
		}
		linkedEligAdminTxDB = db
	})
	return linkedEligAdminTxDB
}

type linkedEligProductRepo struct {
	catalog.ProductRepository
	byID map[string]*catalog.Product
}

func (r *linkedEligProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}
func (r *linkedEligProductRepo) WithTx(*sql.Tx) catalog.ProductRepository { return r }
func (r *linkedEligProductRepo) LockForUpdate(_ context.Context, id string) (catalog.Type, error) {
	if p := r.byID[id]; p != nil {
		return p.Type, nil
	}
	return "", apperror.NotFound("product not found")
}
func (r *linkedEligProductRepo) ListByIDs(_ context.Context, ids []string) (map[string]*catalog.Product, error) {
	out := map[string]*catalog.Product{}
	for _, id := range ids {
		if p := r.byID[id]; p != nil {
			out[id] = p
		}
	}
	return out, nil
}

type linkedEligProductRepoListByIDsErr struct {
	linkedEligProductRepo
}

func (r *linkedEligProductRepoListByIDsErr) ListByIDs(context.Context, []string) (map[string]*catalog.Product, error) {
	return nil, errors.New("db down")
}

type linkedEligCategoryRepo struct {
	catalog.CategoryRepository
	byID map[string]*catalog.Category
}

func (r *linkedEligCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}

type linkedEligRuleRepo struct {
	catalog.LinkedEligibilityRepository
	parents    []catalog.LinkedEligibleParent
	categories []catalog.LinkedEligibleCategory
}

func (r *linkedEligRuleRepo) ListParentsByChildID(context.Context, string) ([]catalog.LinkedEligibleParent, error) {
	return append([]catalog.LinkedEligibleParent{}, r.parents...), nil
}
func (r *linkedEligRuleRepo) ListCategoriesByChildID(context.Context, string) ([]catalog.LinkedEligibleCategory, error) {
	return append([]catalog.LinkedEligibleCategory{}, r.categories...), nil
}
func (r *linkedEligRuleRepo) CountRulesByChildID(context.Context, string) (int, error) {
	return len(r.parents) + len(r.categories), nil
}
func (r *linkedEligRuleRepo) InsertParent(_ context.Context, p *catalog.LinkedEligibleParent) error {
	r.parents = append(r.parents, *p)
	return nil
}
func (r *linkedEligRuleRepo) InsertCategory(_ context.Context, c *catalog.LinkedEligibleCategory) error {
	r.categories = append(r.categories, *c)
	return nil
}
func (r *linkedEligRuleRepo) RemoveParent(_ context.Context, childID, parentID string) error {
	kept := r.parents[:0]
	found := false
	for _, p := range r.parents {
		if p.ChildProductID == childID && p.ParentProductID == parentID {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	r.parents = kept
	if !found {
		return apperror.NotFound("linked eligible parent not found")
	}
	return nil
}
func (r *linkedEligRuleRepo) RemoveCategory(_ context.Context, childID, categoryID string) error {
	kept := r.categories[:0]
	found := false
	for _, c := range r.categories {
		if c.ChildProductID == childID && c.CategoryID == categoryID {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	r.categories = kept
	if !found {
		return apperror.NotFound("linked eligible category not found")
	}
	return nil
}

func (r *linkedEligRuleRepo) ListAllParentSlugRows(context.Context) ([]catalog.LinkedEligibleParentSlugRow, error) {
	return nil, nil
}
func (r *linkedEligRuleRepo) ListAllCategorySlugRows(context.Context) ([]catalog.LinkedEligibleCategorySlugRow, error) {
	return nil, nil
}
func (r *linkedEligRuleRepo) IsEligible(context.Context, string, string) (bool, error) {
	return false, nil
}
func (r *linkedEligRuleRepo) WithTx(*sql.Tx) catalog.LinkedEligibilityRepository { return r }

func newLinkedEligAdminRouter(h *admin.LinkedEligibilityAdminHandler) *http.ServeMux {
	mux := http.NewServeMux()
	withAdmin := admin.AdminContextMiddleware()
	requireRead := admin.RequirePermission(rbac.ProductsRead)
	requireWrite := admin.RequirePermission(rbac.ProductsWrite)
	mux.Handle("GET /api/v1/admin/products/{id}/linked-eligible-parents", withAdmin(requireRead(h.ListParents())))
	mux.Handle("POST /api/v1/admin/products/{id}/linked-eligible-parents", withAdmin(requireWrite(h.AddParent())))
	mux.Handle("DELETE /api/v1/admin/products/{id}/linked-eligible-parents/{parentId}", withAdmin(requireWrite(h.RemoveParent())))
	mux.Handle("GET /api/v1/admin/products/{id}/linked-eligible-categories", withAdmin(requireRead(h.ListCategories())))
	mux.Handle("POST /api/v1/admin/products/{id}/linked-eligible-categories", withAdmin(requireWrite(h.AddCategory())))
	mux.Handle("DELETE /api/v1/admin/products/{id}/linked-eligible-categories/{categoryId}", withAdmin(requireWrite(h.RemoveCategory())))
	return mux
}

func TestLinkedEligibilityAdmin_ListAddRemove(t *testing.T) {
	child := &catalog.Product{ID: "11111111-1111-1111-1111-111111111111", Name: "Ticket", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	parent := &catalog.Product{ID: "22222222-2222-2222-2222-222222222222", Name: "Card", Slug: "card", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	cat := &catalog.Category{ID: "cat-1", Name: "Cards", Slug: "cards"}
	products := &linkedEligProductRepo{byID: map[string]*catalog.Product{child.ID: child, parent.ID: parent}}
	categories := &linkedEligCategoryRepo{byID: map[string]*catalog.Category{cat.ID: cat}}
	rules := &linkedEligRuleRepo{}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, linkedEligAdminTxStarter())
	h := admin.NewLinkedEligibilityAdminHandler(products, categories, svc, adminapp.NewAuditor(logger.New("error")))
	mux := newLinkedEligAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents", nil)
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list parents status = %d body=%s", rec.Code, rec.Body.String())
	}

	body := `{"parent_product_id":"` + parent.ID + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents", strings.NewReader(body))
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add parent status = %d body=%s", rec.Code, rec.Body.String())
	}
	var addResp struct {
		Data struct {
			Parent struct {
				ParentProductID string `json:"parent_product_id"`
				Slug            string `json:"slug"`
			} `json:"parent"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &addResp); err != nil {
		t.Fatalf("decode add: %v", err)
	}
	if addResp.Data.Parent.ParentProductID != parent.ID || addResp.Data.Parent.Slug != "card" {
		t.Fatalf("add parent dto = %+v", addResp.Data.Parent)
	}

	body = `{"category_id":"` + cat.ID + `"}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/"+child.ID+"/linked-eligible-categories", strings.NewReader(body))
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("add category status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents/"+parent.ID, nil)
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove parent status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLinkedEligibilityAdmin_ListParents_ListByIDsErrorStillReturnsRows(t *testing.T) {
	child := &catalog.Product{ID: "11111111-1111-1111-1111-111111111111", Name: "Ticket", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	parentID := "22222222-2222-2222-2222-222222222222"
	products := &linkedEligProductRepoListByIDsErr{
		linkedEligProductRepo: linkedEligProductRepo{byID: map[string]*catalog.Product{child.ID: child}},
	}
	categories := &linkedEligCategoryRepo{byID: map[string]*catalog.Category{}}
	rules := &linkedEligRuleRepo{
		parents: []catalog.LinkedEligibleParent{{
			ChildProductID:  child.ID,
			ParentProductID: parentID,
			CreatedAt:       time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		}},
	}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, linkedEligAdminTxStarter())
	h := admin.NewLinkedEligibilityAdminHandler(products, categories, svc, adminapp.NewAuditor(logger.New("error")))
	mux := newLinkedEligAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents", nil)
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list parents status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Parents []struct {
				ParentProductID string `json:"parent_product_id"`
				Name            string `json:"name"`
				Slug            string `json:"slug"`
				Type            string `json:"type"`
			} `json:"parents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(resp.Data.Parents) != 1 {
		t.Fatalf("parents = %#v, want 1 row", resp.Data.Parents)
	}
	got := resp.Data.Parents[0]
	if got.ParentProductID != parentID || got.Name != "" || got.Slug != "" || got.Type != "" {
		t.Fatalf("enriched dto = %+v, want id only with empty enrichment", got)
	}
}

func TestLinkedEligibilityAdmin_NotFoundAndValidation(t *testing.T) {
	child := &catalog.Product{ID: "11111111-1111-1111-1111-111111111111", Name: "Ticket", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	products := &linkedEligProductRepo{byID: map[string]*catalog.Product{child.ID: child}}
	categories := &linkedEligCategoryRepo{byID: map[string]*catalog.Category{}}
	rules := &linkedEligRuleRepo{}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, linkedEligAdminTxStarter())
	h := admin.NewLinkedEligibilityAdminHandler(products, categories, svc, adminapp.NewAuditor(logger.New("error")))
	mux := newLinkedEligAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products/99999999-9999-9999-9999-999999999999/linked-eligible-parents", nil)
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing child list status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents", strings.NewReader(`{"parent_product_id":""}`))
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty parent status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents", strings.NewReader(`{"parent_product_id":"`+child.ID+`"}`))
	req = testhelper.AuthenticatedRequest(req, "admin-1", identity.RoleAdmin)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("self parent status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLinkedEligibilityAdmin_ForbiddenWithoutPermission(t *testing.T) {
	child := &catalog.Product{ID: "11111111-1111-1111-1111-111111111111", Name: "Ticket", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	products := &linkedEligProductRepo{byID: map[string]*catalog.Product{child.ID: child}}
	categories := &linkedEligCategoryRepo{}
	rules := &linkedEligRuleRepo{}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, linkedEligAdminTxStarter())
	h := admin.NewLinkedEligibilityAdminHandler(products, categories, svc, adminapp.NewAuditor(logger.New("error")))
	mux := newLinkedEligAdminRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/products/"+child.ID+"/linked-eligible-parents", nil)
	req = testhelper.AuthenticatedRequest(req, "cust-1", identity.RoleCustomer)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("customer list status = %d body=%s", rec.Code, rec.Body.String())
	}
}
