package catalog_test

import (
	"context"
	"database/sql"
	"testing"

	catalogApp "github.com/akarso/shopanda/internal/application/catalog"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/apperror"
)

func newLinkedEligibilityService(
	products catalog.ProductRepository,
	categories catalog.CategoryRepository,
	rules catalog.LinkedEligibilityRepository,
) *catalogApp.LinkedEligibilityService {
	if categories == nil {
		categories = &stubCategoryRepo{}
	}
	return catalogApp.NewLinkedEligibilityService(products, categories, rules, testTxStarter())
}

type stubCategoryRepo struct {
	catalog.CategoryRepository
	byID map[string]*catalog.Category
}

func (s *stubCategoryRepo) FindByID(_ context.Context, id string) (*catalog.Category, error) {
	if s.byID == nil {
		return nil, nil
	}
	return s.byID[id], nil
}

type stubLinkedEligibilityRepo struct {
	catalog.LinkedEligibilityRepository
	parents        []catalog.LinkedEligibleParent
	categories     []catalog.LinkedEligibleCategory
	listChildID    string
	insertedParent *catalog.LinkedEligibleParent
	insertedCat    *catalog.LinkedEligibleCategory
	removedParent  struct {
		childID  string
		parentID string
	}
	removedCat struct {
		childID    string
		categoryID string
	}
	removeParentErr error
	removeCatErr    error
	eligible        bool
	eligibleChild   string
	eligibleParent  string
	ruleCount       int
	countChildID    string
	calls           *[]string
}

func (s *stubLinkedEligibilityRepo) ListParentsByChildID(_ context.Context, childProductID string) ([]catalog.LinkedEligibleParent, error) {
	s.listChildID = childProductID
	return s.parents, nil
}

func (s *stubLinkedEligibilityRepo) ListCategoriesByChildID(_ context.Context, childProductID string) ([]catalog.LinkedEligibleCategory, error) {
	s.listChildID = childProductID
	return s.categories, nil
}

func (s *stubLinkedEligibilityRepo) CountRulesByChildID(_ context.Context, childProductID string) (int, error) {
	s.countChildID = childProductID
	return s.ruleCount, nil
}

func (s *stubLinkedEligibilityRepo) InsertParent(_ context.Context, p *catalog.LinkedEligibleParent) error {
	if s.calls != nil {
		*s.calls = append(*s.calls, "insert-parent")
	}
	cp := *p
	s.insertedParent = &cp
	return nil
}

func (s *stubLinkedEligibilityRepo) InsertCategory(_ context.Context, c *catalog.LinkedEligibleCategory) error {
	if s.calls != nil {
		*s.calls = append(*s.calls, "insert-category")
	}
	cp := *c
	s.insertedCat = &cp
	return nil
}

func (s *stubLinkedEligibilityRepo) WithTx(_ *sql.Tx) catalog.LinkedEligibilityRepository { return s }

func (s *stubLinkedEligibilityRepo) RemoveParent(_ context.Context, childProductID, parentProductID string) error {
	s.removedParent.childID = childProductID
	s.removedParent.parentID = parentProductID
	return s.removeParentErr
}

func (s *stubLinkedEligibilityRepo) RemoveCategory(_ context.Context, childProductID, categoryID string) error {
	s.removedCat.childID = childProductID
	s.removedCat.categoryID = categoryID
	return s.removeCatErr
}

func (s *stubLinkedEligibilityRepo) IsEligible(_ context.Context, childProductID, parentProductID string) (bool, error) {
	s.eligibleChild = childProductID
	s.eligibleParent = parentProductID
	return s.eligible, nil
}

func TestNewLinkedEligibilityService_Panics(t *testing.T) {
	products := &stubProductRepo{}
	categories := &stubCategoryRepo{}
	rules := &stubLinkedEligibilityRepo{}
	tx := testTxStarter()

	tests := []struct {
		name string
		fn   func()
	}{
		{"nil products", func() { catalogApp.NewLinkedEligibilityService(nil, categories, rules, tx) }},
		{"nil categories", func() { catalogApp.NewLinkedEligibilityService(products, nil, rules, tx) }},
		{"nil rules", func() { catalogApp.NewLinkedEligibilityService(products, categories, nil, tx) }},
		{"nil tx", func() { catalogApp.NewLinkedEligibilityService(products, categories, rules, nil) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			tc.fn()
		})
	}
}

func TestLinkedEligibilityService_AddParent_LocksBeforeInsert(t *testing.T) {
	var calls []string
	products := &stubProductRepo{
		byID: map[string]*catalog.Product{
			"zzz": {ID: "zzz", Type: catalog.TypeSimple, Status: catalog.StatusActive},
			"aaa": {ID: "aaa", Type: catalog.TypeVirtual, Status: catalog.StatusActive},
		},
		calls: &calls,
	}
	rules := &stubLinkedEligibilityRepo{calls: &calls}
	svc := newLinkedEligibilityService(products, nil, rules)

	p, err := catalog.NewLinkedEligibleParent("zzz", "aaa")
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}
	if err := svc.AddParent(context.Background(), p); err != nil {
		t.Fatalf("AddParent: %v", err)
	}
	want := []string{"lock:aaa", "lock:zzz", "insert-parent"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestLinkedEligibilityService_AddParent_TrimsIDs(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		"p1": {ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	rules := &stubLinkedEligibilityRepo{}
	svc := newLinkedEligibilityService(products, nil, rules)

	p := &catalog.LinkedEligibleParent{ChildProductID: "  c1  ", ParentProductID: " p1 "}
	if err := svc.AddParent(context.Background(), p); err != nil {
		t.Fatalf("AddParent: %v", err)
	}
	if p.ChildProductID != "c1" || p.ParentProductID != "p1" {
		t.Fatalf("ids = %q %q, want trimmed", p.ChildProductID, p.ParentProductID)
	}
	if rules.insertedParent == nil || rules.insertedParent.ChildProductID != "c1" || rules.insertedParent.ParentProductID != "p1" {
		t.Fatalf("insert = %+v", rules.insertedParent)
	}
}

func TestLinkedEligibilityService_AddParent_ProductNotFound(t *testing.T) {
	svc := newLinkedEligibilityService(&stubProductRepo{byID: map[string]*catalog.Product{}}, nil, &stubLinkedEligibilityRepo{})
	p, _ := catalog.NewLinkedEligibleParent("missing", "p1")
	err := svc.AddParent(context.Background(), p)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("AddParent = %v, want not found", err)
	}
}

func TestLinkedEligibilityService_AddParent_MissingParent(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, &stubLinkedEligibilityRepo{})
	p, _ := catalog.NewLinkedEligibleParent("c1", "missing")
	err := svc.AddParent(context.Background(), p)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("AddParent = %v, want not found", err)
	}
}

func TestLinkedEligibilityService_AddParent_NilAndEmpty(t *testing.T) {
	svc := newLinkedEligibilityService(&stubProductRepo{}, nil, &stubLinkedEligibilityRepo{})
	if err := svc.AddParent(context.Background(), nil); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("nil = %v, want validation", err)
	}
	if err := svc.AddParent(context.Background(), &catalog.LinkedEligibleParent{}); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty = %v, want validation", err)
	}
}

func TestLinkedEligibilityService_AddParent_DraftRequiresOperatorScope(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusDraft},
		"p1": {ID: "p1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	rules := &stubLinkedEligibilityRepo{}
	svc := newLinkedEligibilityService(products, nil, rules)
	p, err := catalog.NewLinkedEligibleParent("c1", "p1")
	if err != nil {
		t.Fatalf("NewLinkedEligibleParent: %v", err)
	}

	err = svc.AddParent(context.Background(), p)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("AddParent without scope = %v, want not found", err)
	}
	if rules.insertedParent != nil {
		t.Fatal("insert must not run without operator scope")
	}

	ctx := catalog.WithOperatorProductReadScope(context.Background())
	if err := svc.AddParent(ctx, p); err != nil {
		t.Fatalf("AddParent with operator scope: %v", err)
	}
	if rules.insertedParent == nil {
		t.Fatal("expected insert with operator scope")
	}
}

func TestLinkedEligibilityService_AddCategory_LocksChild(t *testing.T) {
	var calls []string
	products := &stubProductRepo{
		byID: map[string]*catalog.Product{
			"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
		},
		calls: &calls,
	}
	categories := &stubCategoryRepo{byID: map[string]*catalog.Category{
		"cards": {ID: "cards", Name: "Cards"},
	}}
	rules := &stubLinkedEligibilityRepo{calls: &calls}
	svc := newLinkedEligibilityService(products, categories, rules)

	c, err := catalog.NewLinkedEligibleCategory("c1", "cards")
	if err != nil {
		t.Fatalf("NewLinkedEligibleCategory: %v", err)
	}
	if err := svc.AddCategory(context.Background(), c); err != nil {
		t.Fatalf("AddCategory: %v", err)
	}
	want := []string{"lock:c1", "insert-category"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestLinkedEligibilityService_AddCategory_MissingCategory(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	rules := &stubLinkedEligibilityRepo{}
	svc := newLinkedEligibilityService(products, &stubCategoryRepo{}, rules)
	c, _ := catalog.NewLinkedEligibleCategory("c1", "missing")
	err := svc.AddCategory(context.Background(), c)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("AddCategory = %v, want not found", err)
	}
	if rules.insertedCat != nil {
		t.Fatal("insert must not run")
	}
}

func TestLinkedEligibilityService_AddCategory_DraftChildRequiresOperatorScope(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusDraft},
	}}
	categories := &stubCategoryRepo{byID: map[string]*catalog.Category{
		"cards": {ID: "cards", Name: "Cards"},
	}}
	rules := &stubLinkedEligibilityRepo{}
	svc := newLinkedEligibilityService(products, categories, rules)
	c, err := catalog.NewLinkedEligibleCategory("c1", "cards")
	if err != nil {
		t.Fatalf("NewLinkedEligibleCategory: %v", err)
	}

	err = svc.AddCategory(context.Background(), c)
	if !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("AddCategory without scope = %v, want not found", err)
	}
	if rules.insertedCat != nil {
		t.Fatal("insert must not run without operator scope")
	}

	ctx := catalog.WithOperatorProductReadScope(context.Background())
	if err := svc.AddCategory(ctx, c); err != nil {
		t.Fatalf("AddCategory with operator scope: %v", err)
	}
	if rules.insertedCat == nil {
		t.Fatal("expected insert with operator scope")
	}
}

func TestLinkedEligibilityService_AddCategory_TrimsIDs(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	categories := &stubCategoryRepo{byID: map[string]*catalog.Category{
		"cards": {ID: "cards"},
	}}
	rules := &stubLinkedEligibilityRepo{}
	svc := newLinkedEligibilityService(products, categories, rules)

	c := &catalog.LinkedEligibleCategory{ChildProductID: "  c1  ", CategoryID: " cards "}
	if err := svc.AddCategory(context.Background(), c); err != nil {
		t.Fatalf("AddCategory: %v", err)
	}
	if c.ChildProductID != "c1" || c.CategoryID != "cards" {
		t.Fatalf("ids = %q %q, want trimmed", c.ChildProductID, c.CategoryID)
	}
}

func TestLinkedEligibilityService_ListParents(t *testing.T) {
	want := []catalog.LinkedEligibleParent{{ChildProductID: "c1", ParentProductID: "p1"}}
	rules := &stubLinkedEligibilityRepo{parents: want}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, rules)

	got, err := svc.ListParents(context.Background(), "  c1  ")
	if err != nil {
		t.Fatalf("ListParents: %v", err)
	}
	if rules.listChildID != "c1" {
		t.Errorf("listChildID = %q, want c1", rules.listChildID)
	}
	if len(got) != 1 || got[0].ParentProductID != "p1" {
		t.Errorf("ListParents = %+v", got)
	}
}

func TestLinkedEligibilityService_ListParents_NotFoundAndEmpty(t *testing.T) {
	svc := newLinkedEligibilityService(&stubProductRepo{}, nil, &stubLinkedEligibilityRepo{})
	if _, err := svc.ListParents(context.Background(), ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty = %v, want validation", err)
	}
	if _, err := svc.ListParents(context.Background(), "missing"); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("missing = %v, want not found", err)
	}
}

func TestLinkedEligibilityService_ListParents_NilSliceBecomesEmpty(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, &stubLinkedEligibilityRepo{parents: nil})
	got, err := svc.ListParents(context.Background(), "c1")
	if err != nil {
		t.Fatalf("ListParents: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
}

func TestLinkedEligibilityService_ListCategories_NilSliceBecomesEmpty(t *testing.T) {
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, &stubLinkedEligibilityRepo{categories: nil})
	got, err := svc.ListCategories(context.Background(), "c1")
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
}

func TestLinkedEligibilityService_RemoveParent(t *testing.T) {
	rules := &stubLinkedEligibilityRepo{}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, rules)

	if err := svc.RemoveParent(context.Background(), " c1 ", " p1 "); err != nil {
		t.Fatalf("RemoveParent: %v", err)
	}
	if rules.removedParent.childID != "c1" || rules.removedParent.parentID != "p1" {
		t.Errorf("removed = %+v", rules.removedParent)
	}
}

func TestLinkedEligibilityService_RemoveParent_NotFoundAndEmpty(t *testing.T) {
	rules := &stubLinkedEligibilityRepo{removeParentErr: apperror.NotFound("linked eligible parent not found")}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, rules)
	if err := svc.RemoveParent(context.Background(), "c1", "p1"); !apperror.Is(err, apperror.CodeNotFound) {
		t.Fatalf("RemoveParent = %v, want not found", err)
	}
	if err := svc.RemoveParent(context.Background(), "", "p1"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty child = %v, want validation", err)
	}
	if err := svc.RemoveParent(context.Background(), "c1", ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty parent = %v, want validation", err)
	}
}

func TestLinkedEligibilityService_RemoveCategory(t *testing.T) {
	rules := &stubLinkedEligibilityRepo{}
	products := &stubProductRepo{byID: map[string]*catalog.Product{
		"c1": {ID: "c1", Type: catalog.TypeSimple, Status: catalog.StatusActive},
	}}
	svc := newLinkedEligibilityService(products, nil, rules)
	if err := svc.RemoveCategory(context.Background(), " c1 ", " cards "); err != nil {
		t.Fatalf("RemoveCategory: %v", err)
	}
	if rules.removedCat.childID != "c1" || rules.removedCat.categoryID != "cards" {
		t.Errorf("removed = %+v", rules.removedCat)
	}
}

func TestLinkedEligibilityService_IsEligible(t *testing.T) {
	rules := &stubLinkedEligibilityRepo{eligible: true}
	svc := newLinkedEligibilityService(&stubProductRepo{}, nil, rules)

	ok, err := svc.IsEligible(context.Background(), " c1 ", " p1 ")
	if err != nil {
		t.Fatalf("IsEligible: %v", err)
	}
	if !ok {
		t.Fatal("expected eligible")
	}
	if rules.eligibleChild != "c1" || rules.eligibleParent != "p1" {
		t.Fatalf("forwarded = %q %q", rules.eligibleChild, rules.eligibleParent)
	}
}

func TestLinkedEligibilityService_IsEligible_SelfAndEmpty(t *testing.T) {
	rules := &stubLinkedEligibilityRepo{eligible: true}
	svc := newLinkedEligibilityService(&stubProductRepo{}, nil, rules)

	ok, err := svc.IsEligible(context.Background(), "same", "same")
	if err != nil {
		t.Fatalf("self: %v", err)
	}
	if ok {
		t.Fatal("self must not be eligible")
	}
	if rules.eligibleChild != "" {
		t.Fatal("repo must not be called for self")
	}
	if _, err := svc.IsEligible(context.Background(), "", "p1"); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty child = %v, want validation", err)
	}
	if _, err := svc.IsEligible(context.Background(), "c1", ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("empty parent = %v, want validation", err)
	}
}

func TestLinkedEligibilityService_HasRules(t *testing.T) {
	rules := &stubLinkedEligibilityRepo{ruleCount: 2}
	svc := newLinkedEligibilityService(&stubProductRepo{}, nil, rules)

	ok, err := svc.HasRules(context.Background(), "  c1  ")
	if err != nil {
		t.Fatalf("HasRules: %v", err)
	}
	if !ok {
		t.Fatal("expected rules")
	}
	if rules.countChildID != "c1" {
		t.Fatalf("countChildID = %q", rules.countChildID)
	}

	rules.ruleCount = 0
	ok, err = svc.HasRules(context.Background(), "missing")
	if err != nil {
		t.Fatalf("HasRules missing child: %v", err)
	}
	if ok {
		t.Fatal("missing child with no rows must be false, not NotFound")
	}
}

func TestLinkedEligibilityService_HasRules_EmptyID(t *testing.T) {
	svc := newLinkedEligibilityService(&stubProductRepo{}, nil, &stubLinkedEligibilityRepo{})
	if _, err := svc.HasRules(context.Background(), ""); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("HasRules empty = %v, want validation", err)
	}
	if _, err := svc.HasRules(context.Background(), "  "); !apperror.Is(err, apperror.CodeValidation) {
		t.Fatalf("HasRules blank = %v, want validation", err)
	}
}
