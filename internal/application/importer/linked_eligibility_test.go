package importer_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

type linkedImportRuleRepo struct {
	catalog.LinkedEligibilityRepository
	parents    []catalog.LinkedEligibleParent
	categories []catalog.LinkedEligibleCategory
}

func (r *linkedImportRuleRepo) ListParentsByChildID(context.Context, string) ([]catalog.LinkedEligibleParent, error) {
	return nil, nil
}
func (r *linkedImportRuleRepo) ListCategoriesByChildID(context.Context, string) ([]catalog.LinkedEligibleCategory, error) {
	return nil, nil
}
func (r *linkedImportRuleRepo) CountRulesByChildID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *linkedImportRuleRepo) InsertParent(_ context.Context, p *catalog.LinkedEligibleParent) error {
	r.parents = append(r.parents, *p)
	return nil
}
func (r *linkedImportRuleRepo) InsertCategory(_ context.Context, c *catalog.LinkedEligibleCategory) error {
	r.categories = append(r.categories, *c)
	return nil
}
func (r *linkedImportRuleRepo) RemoveParent(context.Context, string, string) error   { return nil }
func (r *linkedImportRuleRepo) RemoveCategory(context.Context, string, string) error { return nil }

func (r *linkedImportRuleRepo) ListAllParentSlugRows(context.Context) ([]catalog.LinkedEligibleParentSlugRow, error) {
	return nil, nil
}
func (r *linkedImportRuleRepo) ListAllCategorySlugRows(context.Context) ([]catalog.LinkedEligibleCategorySlugRow, error) {
	return nil, nil
}
func (r *linkedImportRuleRepo) IsEligible(context.Context, string, string) (bool, error) {
	return false, nil
}
func (r *linkedImportRuleRepo) WithTx(*sql.Tx) catalog.LinkedEligibilityRepository { return r }

type linkedEligImportCategoryRepo struct {
	catalog.CategoryRepository
	bySlug map[string]*catalog.Category
	byID   map[string]*catalog.Category
}

func (r *linkedEligImportCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	if r.byID != nil {
		return r.byID[id], nil
	}
	for _, c := range r.bySlug {
		if c != nil && c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}

func (r *linkedEligImportCategoryRepo) FindBySlug(_ context.Context, slug string) (*catalog.Category, error) {
	if r.bySlug == nil {
		return nil, nil
	}
	return r.bySlug[slug], nil
}

func TestLinkedEligibleParentImporter_Import(t *testing.T) {
	child := &catalog.Product{ID: "c1", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	parent := &catalog.Product{ID: "p1", Slug: "card", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{"ticket": child, "card": parent}}
	rules := &linkedImportRuleRepo{}
	categories := &linkedEligImportCategoryRepo{}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, compositionTxStarter())
	imp := importer.NewLinkedEligibleParentImporter(products, svc)
	csv := "child_slug,parent_slug\nticket,card\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(rules.parents) != 1 || rules.parents[0].ChildProductID != "c1" || rules.parents[0].ParentProductID != "p1" {
		t.Fatalf("parents = %+v", rules.parents)
	}
}

func TestLinkedEligibleCategoryImporter_Import(t *testing.T) {
	child := &catalog.Product{ID: "c1", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	cat := &catalog.Category{ID: "cat-1", Name: "Cards", Slug: "cards"}
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{"ticket": child}}
	rules := &linkedImportRuleRepo{}
	categories := &linkedEligImportCategoryRepo{
		bySlug: map[string]*catalog.Category{"cards": cat},
		byID:   map[string]*catalog.Category{cat.ID: cat},
	}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, compositionTxStarter())
	imp := importer.NewLinkedEligibleCategoryImporter(products, categories, svc)
	csv := "child_slug,category_slug\nticket,cards\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(rules.categories) != 1 || rules.categories[0].ChildProductID != "c1" || rules.categories[0].CategoryID != "cat-1" {
		t.Fatalf("categories = %+v", rules.categories)
	}
}

func TestLinkedEligibleParentImporter_UnknownSlugSelfParentMissingHeader(t *testing.T) {
	child := &catalog.Product{ID: "c1", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{"ticket": child}}
	rules := &linkedImportRuleRepo{}
	categories := &linkedEligImportCategoryRepo{}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, compositionTxStarter())
	imp := importer.NewLinkedEligibleParentImporter(products, svc)

	if _, err := imp.Import(context.Background(), strings.NewReader("parent_slug\ncard\n")); err == nil || !strings.Contains(err.Error(), "missing required column") {
		t.Fatalf("missing header err = %v", err)
	}

	result, err := imp.Import(context.Background(), strings.NewReader("child_slug,parent_slug\nticket,missing\nticket,ticket\n"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 0 || result.Skipped < 2 {
		t.Fatalf("result = %+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "unknown parent slug") {
		t.Fatalf("errors = %v, want unknown parent", result.Errors)
	}
	if !strings.Contains(joined, "itself") && !strings.Contains(joined, "eligible parent") {
		t.Fatalf("errors = %v, want self-parent validation", result.Errors)
	}
}

func TestLinkedEligibleCategoryImporter_UnknownSlugMissingHeader(t *testing.T) {
	child := &catalog.Product{ID: "c1", Slug: "ticket", Type: catalog.TypeSimple, Status: catalog.StatusActive}
	products := &compositionProductRepo{bySlug: map[string]*catalog.Product{"ticket": child}}
	rules := &linkedImportRuleRepo{}
	categories := &linkedEligImportCategoryRepo{bySlug: map[string]*catalog.Category{}}
	svc := catalogApp.NewLinkedEligibilityService(products, categories, rules, compositionTxStarter())
	imp := importer.NewLinkedEligibleCategoryImporter(products, categories, svc)

	if _, err := imp.Import(context.Background(), strings.NewReader("child_slug\nticket\n")); err == nil || !strings.Contains(err.Error(), "missing required column") {
		t.Fatalf("missing header err = %v", err)
	}

	result, err := imp.Import(context.Background(), strings.NewReader("child_slug,category_slug\nticket,nope\n"))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(strings.Join(result.Errors, "\n"), "unknown category slug") {
		t.Fatalf("errors = %v", result.Errors)
	}
}
