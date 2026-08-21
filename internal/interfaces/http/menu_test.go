package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"

	shophttp "github.com/akarso/shopanda/internal/interfaces/http"
)

type menuTestCategoryRepo struct {
	byID map[string]*catalog.Category
}

func (s menuTestCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	return s.byID[id], nil
}
func (menuTestCategoryRepo) FindBySlug(context.Context, string) (*catalog.Category, error) {
	return nil, nil
}
func (menuTestCategoryRepo) FindByParentID(context.Context, *string) ([]catalog.Category, error) {
	return nil, nil
}
func (menuTestCategoryRepo) FindAll(context.Context) ([]catalog.Category, error) { return nil, nil }
func (menuTestCategoryRepo) Create(context.Context, *catalog.Category) error     { return nil }
func (menuTestCategoryRepo) Update(context.Context, *catalog.Category) error     { return nil }
func (menuTestCategoryRepo) Delete(context.Context, string) error                { return nil }

type menuTestPageRepo struct {
	byID map[string]*cms.Page
}

func (s menuTestPageRepo) FindByID(_ context.Context, id string) (*cms.Page, error) {
	return s.byID[id], nil
}
func (menuTestPageRepo) FindBySlug(context.Context, string) (*cms.Page, error) { return nil, nil }
func (menuTestPageRepo) FindActiveBySlug(context.Context, string) (*cms.Page, error) {
	return nil, nil
}
func (menuTestPageRepo) List(context.Context, int, int) ([]*cms.Page, error) { return nil, nil }
func (menuTestPageRepo) Create(context.Context, *cms.Page) error             { return nil }
func (menuTestPageRepo) Update(context.Context, *cms.Page) error             { return nil }
func (menuTestPageRepo) Delete(context.Context, string) error                { return nil }

type mockMenuRepo struct {
	listFn       func(ctx context.Context) ([]*cms.Menu, error)
	findByIDFn   func(ctx context.Context, id string) (*cms.MenuWithItems, error)
	findByCodeFn func(ctx context.Context, code string) (*cms.MenuWithItems, error)
	saveFn       func(ctx context.Context, menu *cms.MenuWithItems) error
}

func (m *mockMenuRepo) List(ctx context.Context) ([]*cms.Menu, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return nil, nil
}

func (m *mockMenuRepo) FindByID(ctx context.Context, id string) (*cms.MenuWithItems, error) {
	if m.findByIDFn != nil {
		return m.findByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockMenuRepo) FindByCode(ctx context.Context, code string) (*cms.MenuWithItems, error) {
	if m.findByCodeFn != nil {
		return m.findByCodeFn(ctx, code)
	}
	return nil, nil
}

func (m *mockMenuRepo) Save(ctx context.Context, menu *cms.MenuWithItems) error {
	if m.saveFn != nil {
		return m.saveFn(ctx, menu)
	}
	return nil
}

func testMenuResolver() *cmsApp.MenuResolver {
	cat, _ := catalog.NewCategory("cat-1", "Headphones", "headphones")
	page, _ := cms.NewPage("page-1", "about", "About", "")
	return cmsApp.NewMenuResolver(
		menuTestCategoryRepo{byID: map[string]*catalog.Category{"cat-1": &cat}},
		menuTestPageRepo{byID: map[string]*cms.Page{page.ID(): page}},
	)
}

func TestMenuHandlerGetByCode(t *testing.T) {
	now := time.Now().UTC()
	menu := cms.NewMenuFromDB("menu-header", "header", "Header", true, now, now)
	item, _ := cms.NewMenuItem("item-1", menu.ID(), "", "About", cms.LinkTypePage, "page-1", 0)

	repo := &mockMenuRepo{
		findByCodeFn: func(_ context.Context, code string) (*cms.MenuWithItems, error) {
			if code != "header" {
				return nil, nil
			}
			return &cms.MenuWithItems{Menu: menu, Items: []*cms.MenuItem{item}}, nil
		},
	}
	h := shophttp.NewMenuHandler(repo, testMenuResolver())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/menus/{code}", h.GetByCode())

	req := httptest.NewRequest("GET", "/api/v1/menus/header", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Code  string `json:"code"`
			Items []struct {
				Label string `json:"label"`
				URL   string `json:"url"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	resp := envelope.Data
	if resp.Code != "header" || len(resp.Items) != 1 || resp.Items[0].URL != "/pages/about" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
