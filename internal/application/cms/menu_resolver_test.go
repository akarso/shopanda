package cms_test

import (
	"context"
	"testing"
	"time"

	cmsApp "github.com/akarso/shopanda/internal/application/cms"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/cms"
)

type stubCategoryRepo struct {
	byID map[string]*catalog.Category
}

func (s stubCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	return s.byID[id], nil
}

func (s stubCategoryRepo) FindBySlug(context.Context, string) (*catalog.Category, error) {
	return nil, nil
}
func (s stubCategoryRepo) FindByParentID(context.Context, *string) ([]catalog.Category, error) {
	return nil, nil
}
func (s stubCategoryRepo) FindAll(context.Context) ([]catalog.Category, error) { return nil, nil }
func (s stubCategoryRepo) Create(context.Context, *catalog.Category) error     { return nil }
func (s stubCategoryRepo) Update(context.Context, *catalog.Category) error     { return nil }
func (s stubCategoryRepo) Delete(context.Context, string) error                { return nil }

type stubPageRepo struct {
	byID map[string]*cms.Page
}

func (s stubPageRepo) FindByID(_ context.Context, id string) (*cms.Page, error) {
	return s.byID[id], nil
}
func (s stubPageRepo) FindBySlug(context.Context, string) (*cms.Page, error) { return nil, nil }
func (s stubPageRepo) FindActiveBySlug(context.Context, string) (*cms.Page, error) {
	return nil, nil
}
func (s stubPageRepo) List(context.Context, int, int) ([]*cms.Page, error) { return nil, nil }
func (s stubPageRepo) Create(context.Context, *cms.Page) error             { return nil }
func (s stubPageRepo) Update(context.Context, *cms.Page) error             { return nil }
func (s stubPageRepo) Delete(context.Context, string) error                { return nil }

func TestMenuResolverResolveTree(t *testing.T) {
	cat, _ := catalog.NewCategory("cat-1", "Headphones", "headphones")
	page, _ := cms.NewPage("page-1", "about", "About", "content")
	now := time.Now().UTC()

	resolver := cmsApp.NewMenuResolver(
		stubCategoryRepo{byID: map[string]*catalog.Category{"cat-1": &cat}},
		stubPageRepo{byID: map[string]*cms.Page{page.ID(): page}},
	)

	root, _ := cms.NewMenuItem("root", "menu-1", "", "Shop", cms.LinkTypeURL, "/shop", 0)
	childCat, _ := cms.NewMenuItem("child-cat", "menu-1", "root", "Headphones", cms.LinkTypeCategory, "cat-1", 1)
	childPage, _ := cms.NewMenuItem("child-page", "menu-1", "root", "About", cms.LinkTypePage, page.ID(), 2)
	inactive, _ := cms.NewMenuItem("inactive", "menu-1", "", "Hidden", cms.LinkTypeURL, "/hidden", 3)
	inactive = cms.NewMenuItemFromDB(
		inactive.ID(), inactive.MenuID(), inactive.ParentID(), inactive.Label(),
		inactive.LinkType(), inactive.LinkTarget(), inactive.Position(),
		false, now, now,
	)

	tree, err := resolver.ResolveTree(context.Background(), []*cms.MenuItem{root, childCat, childPage, inactive})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(tree) != 1 {
		t.Fatalf("expected one root, got %d", len(tree))
	}
	if tree[0].URL != "/shop" || len(tree[0].Children) != 2 {
		t.Fatalf("unexpected root: %+v", tree[0])
	}
	if tree[0].Children[0].URL != "/categories/headphones" {
		t.Fatalf("category url: %s", tree[0].Children[0].URL)
	}
	if tree[0].Children[1].URL != "/pages/about" {
		t.Fatalf("page url: %s", tree[0].Children[1].URL)
	}
}

func TestMenuResolverLoadsTargetsOnce(t *testing.T) {
	cat, _ := catalog.NewCategory("cat-1", "Headphones", "headphones")
	calls := 0
	categories := countingCategoryRepo{
		category: &cat,
		onFind:   func() { calls++ },
	}
	resolver := cmsApp.NewMenuResolver(categories, stubPageRepo{byID: map[string]*cms.Page{}})

	itemA, _ := cms.NewMenuItem("a", "menu-1", "", "A", cms.LinkTypeCategory, "cat-1", 0)
	itemB, _ := cms.NewMenuItem("b", "menu-1", "", "B", cms.LinkTypeCategory, "cat-1", 1)
	_, err := resolver.ResolveTree(context.Background(), []*cms.MenuItem{itemA, itemB})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if calls != 1 {
		t.Fatalf("FindByID calls = %d, want 1", calls)
	}
}

type countingCategoryRepo struct {
	category *catalog.Category
	onFind   func()
}

func (c countingCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	if c.onFind != nil {
		c.onFind()
	}
	if id == c.category.ID {
		return c.category, nil
	}
	return nil, nil
}
func (countingCategoryRepo) FindBySlug(context.Context, string) (*catalog.Category, error) {
	return nil, nil
}
func (countingCategoryRepo) FindByParentID(context.Context, *string) ([]catalog.Category, error) {
	return nil, nil
}
func (countingCategoryRepo) FindAll(context.Context) ([]catalog.Category, error) { return nil, nil }
func (countingCategoryRepo) Create(context.Context, *catalog.Category) error     { return nil }
func (countingCategoryRepo) Update(context.Context, *catalog.Category) error     { return nil }
func (countingCategoryRepo) Delete(context.Context, string) error                { return nil }
