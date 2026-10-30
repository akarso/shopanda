package importer_test

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/akarso/shopanda/internal/testutil"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- mocks ---

type mockProductRepo struct {
	findBySlugFn func(ctx context.Context, slug string) (*catalog.Product, error)
	createFn     func(ctx context.Context, p *catalog.Product) error
	products     []*catalog.Product
}

func (m *mockProductRepo) FindByID(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) FindBySlug(ctx context.Context, slug string) (*catalog.Product, error) {
	if m.findBySlugFn != nil {
		return m.findBySlugFn(ctx, slug)
	}
	return nil, nil
}
func (m *mockProductRepo) List(_ context.Context, _ catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) Create(ctx context.Context, p *catalog.Product) error {
	if m.createFn != nil {
		return m.createFn(ctx, p)
	}
	m.products = append(m.products, p)
	return nil
}
func (m *mockProductRepo) Update(_ context.Context, _ *catalog.Product) error {
	return nil
}
func (m *mockProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository { return m }

type mockVariantRepo struct {
	createFn func(ctx context.Context, v *catalog.Variant) error
	variants []*catalog.Variant
}

func (m *mockVariantRepo) FindByID(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) FindBySKUs(_ context.Context, _ []string) (map[string]*catalog.Variant, error) {
	return map[string]*catalog.Variant{}, nil
}
func (m *mockVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, m.ListByProductID, productIDs, limitPerProduct)
}

func (m *mockVariantRepo) Create(ctx context.Context, v *catalog.Variant) error {
	if m.createFn != nil {
		return m.createFn(ctx, v)
	}
	m.variants = append(m.variants, v)
	return nil
}
func (m *mockVariantRepo) Update(_ context.Context, _ *catalog.Variant) error {
	return nil
}
func (m *mockVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository { return m }

// --- tests ---

func TestImport_ResolvesExistingDraftBySlug(t *testing.T) {
	draft := &catalog.Product{
		ID:     "prod-draft",
		Name:   "Widget",
		Slug:   "widget",
		Status: catalog.StatusDraft,
		Type:   catalog.TypeSimple,
	}
	createCalls := 0
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug != "widget" {
				return nil, nil
			}
			if !catalog.IncludeNonActiveProducts(ctx) {
				t.Fatal("Import must opt into IncludeNonActiveProducts for slug lookup")
			}
			if !catalog.BypassProductVisibility(ctx) {
				t.Fatal("Import must set BypassProductVisibility (WithOperatorProductReadScope)")
			}
			return draft, nil
		},
		createFn: func(context.Context, *catalog.Product) error {
			createCalls++
			return nil
		},
	}
	varRepo := &mockVariantRepo{}
	csv := `name,slug,sku,description,variant_name
Widget,widget,SKU-NEW,,Default
`
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if createCalls != 0 {
		t.Fatalf("Create products = %d, want 0 (existing draft slug)", createCalls)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
	if len(varRepo.variants) != 1 || varRepo.variants[0].ProductID != draft.ID {
		t.Fatalf("variants = %+v, want one variant on draft product", varRepo.variants)
	}
}

func TestImport_BasicCSV(t *testing.T) {
	csv := `name,slug,sku,description,variant_name
Widget,widget,SKU-001,A fine widget,Size S
Widget,widget,SKU-002,,Size M
Gadget,gadget,SKU-003,A cool gadget,Default
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Products != 2 {
		t.Errorf("Products = %d, want 2", result.Products)
	}
	if result.Variants != 3 {
		t.Errorf("Variants = %d, want 3", result.Variants)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", result.Errors)
	}

	// Check products created.
	if len(prodRepo.products) != 2 {
		t.Fatalf("products created = %d, want 2", len(prodRepo.products))
	}
	bySlug := make(map[string]*catalog.Product, len(prodRepo.products))
	for _, p := range prodRepo.products {
		bySlug[p.Slug] = p
	}
	widget := bySlug["widget"]
	if widget == nil {
		t.Fatal("widget product not created")
	}
	if widget.Name != "Widget" {
		t.Errorf("widget.Name = %q, want Widget", widget.Name)
	}
	if widget.Description != "A fine widget" {
		t.Errorf("widget.Description = %q, want 'A fine widget'", widget.Description)
	}
	if widget.Type != catalog.TypeSimple {
		t.Errorf("widget.Type = %q, want simple (no type column)", widget.Type)
	}
	gadget := bySlug["gadget"]
	if gadget == nil {
		t.Fatal("gadget product not created")
	}
	if gadget.Name != "Gadget" {
		t.Errorf("gadget.Name = %q, want Gadget", gadget.Name)
	}

	// Check variants.
	if len(varRepo.variants) != 3 {
		t.Fatalf("variants created = %d, want 3", len(varRepo.variants))
	}
	bySKU := make(map[string]*catalog.Variant, len(varRepo.variants))
	for _, v := range varRepo.variants {
		bySKU[v.SKU] = v
	}
	v1, v2, v3 := bySKU["SKU-001"], bySKU["SKU-002"], bySKU["SKU-003"]
	if v1 == nil || v2 == nil || v3 == nil {
		t.Fatalf("variants by SKU = %+v", bySKU)
	}
	if v1.ProductID != v2.ProductID {
		t.Error("SKU-001 and SKU-002 should share product ID")
	}
	if v3.ProductID == v1.ProductID {
		t.Error("SKU-003 should have a different product ID")
	}
	if v1.Name != "Size S" {
		t.Errorf("SKU-001 name = %q, want 'Size S'", v1.Name)
	}
}

func TestImport_WithType(t *testing.T) {
	csv := `name,slug,sku,type
E-book,e-book,SKU-EB,virtual
Card,card,SKU-CD,downloadable
Plain,plain,SKU-PL,
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 3 || result.Skipped != 0 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v, want 3 products and no errors", result)
	}
	bySlug := make(map[string]*catalog.Product, len(prodRepo.products))
	for _, p := range prodRepo.products {
		bySlug[p.Slug] = p
	}
	if bySlug["e-book"].Type != catalog.TypeVirtual {
		t.Errorf("e-book type = %q, want virtual", bySlug["e-book"].Type)
	}
	if bySlug["card"].Type != catalog.TypeDownloadable {
		t.Errorf("card type = %q, want downloadable", bySlug["card"].Type)
	}
	if bySlug["plain"].Type != catalog.TypeSimple {
		t.Errorf("plain type = %q, want simple (empty column)", bySlug["plain"].Type)
	}
}

func TestImport_TypeCaseSensitiveLikeAdmin(t *testing.T) {
	csv := `name,slug,sku,type
E-book,e-book,SKU-EB,Virtual
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 {
		t.Errorf("Products = %d, want 0 (Virtual must fail like admin API)", result.Products)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, `invalid product type "Virtual"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Errors = %v, want invalid product type Virtual", result.Errors)
	}
}

func TestImport_ConflictingTypesInSlugGroup(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-1,virtual
Widget,widget,SKU-2,simple
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || len(prodRepo.products) != 0 {
		t.Fatalf("want no products created, got result=%+v products=%d", result, len(prodRepo.products))
	}
	if result.Skipped < 2 {
		t.Errorf("Skipped = %d, want >= 2 (both rows)", result.Skipped)
	}
	conflict := 0
	for _, e := range result.Errors {
		if strings.Contains(e, "conflicting type values") && strings.Contains(e, "simple") && strings.Contains(e, "virtual") {
			conflict++
		}
	}
	if conflict < 2 {
		t.Fatalf("Errors = %v, want conflicting type listing simple and virtual on both lines", result.Errors)
	}
}

func TestImport_EmptyTypeMixedWithVirtualConflicts(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-1,virtual
Widget,widget,SKU-2,
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || len(prodRepo.products) != 0 {
		t.Fatalf("want no products created, got result=%+v", result)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "conflicting type values") && strings.Contains(e, "simple") && strings.Contains(e, "virtual") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Errors = %v, want empty+virtual conflict (blank counts as simple)", result.Errors)
	}
}

func TestImport_EmptyTypeMixedWithSimpleAgrees(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-1,simple
Widget,widget,SKU-2,
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || result.Variants != 2 {
		t.Fatalf("result = %+v, want 1 product / 2 variants", result)
	}
	if prodRepo.products[0].Type != catalog.TypeSimple {
		t.Errorf("type = %q, want simple", prodRepo.products[0].Type)
	}
}

func TestImport_ExistingProductTypeMismatchRejected(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-NEW,virtual
`
	existing := &catalog.Product{ID: "existing-id", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want no variants created on type mismatch, got result=%+v", result)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "type updates via CSV are not supported") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Errors = %v, want type updates not supported", result.Errors)
	}
}

func TestImport_ExistingProductMatchingTypeAllowed(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-NEW,simple
`
	existing := &catalog.Product{ID: "existing-id", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
}

func TestImport_ExistingProductEmptyTypeColumnAddsVariants(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-NEW,
`
	existing := &catalog.Product{ID: "existing-id", Name: "Widget", Slug: "widget", Type: catalog.TypeVirtual}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1 (empty type must not block variant add)", result.Variants)
	}
	if existing.Type != catalog.TypeVirtual {
		t.Errorf("stored type mutated to %q, want virtual", existing.Type)
	}
}

func TestImport_InvalidTypeSkipsSiblingRows(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-1,simple
Widget,widget,SKU-2,kit
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 {
		t.Errorf("Products = %d, want 0", result.Products)
	}
	if result.Skipped < 2 {
		t.Errorf("Skipped = %d, want >= 2", result.Skipped)
	}
	sibling := false
	for _, e := range result.Errors {
		if strings.Contains(e, "skipped because another row for slug") {
			sibling = true
		}
	}
	if !sibling {
		t.Fatalf("Errors = %v, want sibling skip message", result.Errors)
	}
}

func TestImport_WhitespaceOnlyTypeDefaultsSimple(t *testing.T) {
	csv := "name,slug,sku,type\nPlain,plain,SKU-PL,   \n"
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || len(prodRepo.products) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if prodRepo.products[0].Type != catalog.TypeSimple {
		t.Errorf("type = %q, want simple", prodRepo.products[0].Type)
	}
}

func TestImport_BundleType(t *testing.T) {
	csv := `name,slug,sku,type
Bundle,bundle,SKU-B,bundle
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || prodRepo.products[0].Type != catalog.TypeBundle {
		t.Fatalf("result = %+v type=%v", result, prodRepo.products)
	}
}

func TestImport_InvalidType(t *testing.T) {
	csv := `name,slug,sku,type
Good,good,SKU-G,simple
Bad,bad,SKU-B,kit
Also,also,SKU-A,virtual
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 2 {
		t.Errorf("Products = %d, want 2 (good + also)", result.Products)
	}
	if result.Skipped < 1 {
		t.Errorf("Skipped = %d, want >= 1", result.Skipped)
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "line 3:") && strings.Contains(e, `invalid product type "kit"`) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Errors = %v, want line 3 invalid product type kit", result.Errors)
	}
	for _, p := range prodRepo.products {
		if p.Slug == "bad" {
			t.Fatal("invalid-type row must not create a product")
		}
	}
}

func TestImport_MatchingTypesAcrossSlugGroup(t *testing.T) {
	csv := `name,slug,sku,type
Widget,widget,SKU-1,configurable
Widget,widget,SKU-2,configurable
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || result.Variants != 2 {
		t.Fatalf("result = %+v", result)
	}
	if prodRepo.products[0].Type != catalog.TypeConfigurable {
		t.Errorf("type = %q, want configurable", prodRepo.products[0].Type)
	}
}

func TestImport_ExistingProduct(t *testing.T) {
	csv := `name,slug,sku
Widget,widget,SKU-NEW
`
	existing := &catalog.Product{ID: "existing-id", Name: "Widget", Slug: "widget"}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Products != 0 {
		t.Errorf("Products = %d, want 0 (product already exists)", result.Products)
	}
	if result.Variants != 1 {
		t.Errorf("Variants = %d, want 1", result.Variants)
	}
	if len(prodRepo.products) != 0 {
		t.Errorf("products created = %d, want 0", len(prodRepo.products))
	}
	if varRepo.variants[0].ProductID != "existing-id" {
		t.Errorf("variant.ProductID = %q, want existing-id", varRepo.variants[0].ProductID)
	}
}

func TestImport_MissingRequiredColumn(t *testing.T) {
	csv := `name,slug
Widget,widget
`
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)

	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing sku column")
	}
}

func TestImport_EmptyRequiredFields(t *testing.T) {
	csv := `name,slug,sku
,widget,SKU-1
Widget,,SKU-2
Widget,gadget,
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if result.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3", result.Skipped)
	}
	if result.Products != 0 {
		t.Errorf("Products = %d, want 0", result.Products)
	}
	if result.Variants != 0 {
		t.Errorf("Variants = %d, want 0", result.Variants)
	}
	if len(result.Errors) != 3 {
		t.Errorf("len(Errors) = %d, want 3", len(result.Errors))
	}
}

func TestImport_EmptyCSV(t *testing.T) {
	csv := `name,slug,sku
`
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || result.Variants != 0 || result.Skipped != 0 {
		t.Errorf("expected all zeros, got products=%d variants=%d skipped=%d",
			result.Products, result.Variants, result.Skipped)
	}
}

func TestImport_ColumnOrderDoesNotMatter(t *testing.T) {
	csv := `sku,slug,description,name,variant_name
SKU-1,widget,,Widget,Size S
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 {
		t.Errorf("Products = %d, want 1", result.Products)
	}
	if result.Variants != 1 {
		t.Errorf("Variants = %d, want 1", result.Variants)
	}
	if prodRepo.products[0].Name != "Widget" {
		t.Errorf("product name = %q, want Widget", prodRepo.products[0].Name)
	}
}

func TestImport_DuplicateSKUReportsError(t *testing.T) {
	csv := `name,slug,sku
Widget,widget,SKU-DUP
Widget,widget,SKU-DUP
`
	callCount := 0
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{
		createFn: func(_ context.Context, v *catalog.Variant) error {
			callCount++
			if callCount == 2 {
				return fmt.Errorf("duplicate sku")
			}
			return nil
		},
	}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Errorf("Variants = %d, want 1", result.Variants)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestImport_NoHeader(t *testing.T) {
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)

	_, err := imp.Import(context.Background(), strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

// --- attribute import tests ---

func TestImport_AttributeColumnsNoRegistry(t *testing.T) {
	csv := `name,slug,sku,color,weight
Widget,widget,SKU-001,Red,1.5
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
	attrs := varRepo.variants[0].Attributes
	if attrs == nil {
		t.Fatal("variant.Attributes is nil, want populated")
	}
	if attrs["color"] != "Red" {
		t.Errorf("color = %v, want Red", attrs["color"])
	}
	if attrs["weight"] != "1.5" {
		t.Errorf("weight = %v, want 1.5 (string, no registry)", attrs["weight"])
	}
}

func TestImport_AttributeTypeParsing(t *testing.T) {
	csv := `name,slug,sku,color,weight,featured
Widget,widget,SKU-001,Red,2.5,yes
`
	reg := catalog.NewAttributeRegistry()
	colorAttr, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	weightAttr, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	featuredAttr, _ := catalog.NewAttribute("featured", "Featured", catalog.AttributeTypeBoolean)
	reg.RegisterAttribute(colorAttr)
	reg.RegisterAttribute(weightAttr)
	reg.RegisterAttribute(featuredAttr)

	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", result.Errors)
	}
	attrs := varRepo.variants[0].Attributes
	if attrs["color"] != "Red" {
		t.Errorf("color = %v, want Red", attrs["color"])
	}
	if v, ok := attrs["weight"].(float64); !ok || v != 2.5 {
		t.Errorf("weight = %v (%T), want 2.5 (float64)", attrs["weight"], attrs["weight"])
	}
	if v, ok := attrs["featured"].(bool); !ok || !v {
		t.Errorf("featured = %v (%T), want true (bool)", attrs["featured"], attrs["featured"])
	}
}

func TestImport_AttributeNumberParseError(t *testing.T) {
	csv := `name,slug,sku,weight
Widget,widget,SKU-001,notanumber
`
	reg := catalog.NewAttributeRegistry()
	weightAttr, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	reg.RegisterAttribute(weightAttr)

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0], "not a valid number") {
		t.Errorf("error = %q, want containing 'not a valid number'", result.Errors[0])
	}
}

func TestImport_AttributeBooleanValues(t *testing.T) {
	csv := `name,slug,sku,active
Widget,widget,S1,true
Widget,widget,S2,false
Widget,widget,S3,1
Widget,widget,S4,0
Widget,widget,S5,yes
Widget,widget,S6,no
`
	reg := catalog.NewAttributeRegistry()
	attr, _ := catalog.NewAttribute("active", "Active", catalog.AttributeTypeBoolean)
	reg.RegisterAttribute(attr)

	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", result.Errors)
	}
	if result.Variants != 6 {
		t.Fatalf("Variants = %d, want 6", result.Variants)
	}
	expected := map[string]bool{"S1": true, "S2": false, "S3": true, "S4": false, "S5": true, "S6": false}
	for _, v := range varRepo.variants {
		want, ok := expected[v.SKU]
		if !ok {
			t.Errorf("unexpected SKU %q", v.SKU)
			continue
		}
		got, ok := v.Attributes["active"].(bool)
		if !ok {
			t.Errorf("variant %s active is %T, want bool", v.SKU, v.Attributes["active"])
			continue
		}
		if got != want {
			t.Errorf("variant %s active = %v, want %v", v.SKU, got, want)
		}
	}
}

func TestImport_AttributeBooleanParseError(t *testing.T) {
	csv := `name,slug,sku,active
Widget,widget,SKU-001,maybe
`
	reg := catalog.NewAttributeRegistry()
	attr, _ := catalog.NewAttribute("active", "Active", catalog.AttributeTypeBoolean)
	reg.RegisterAttribute(attr)

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0], "not a valid boolean") {
		t.Errorf("error = %q, want containing 'not a valid boolean'", result.Errors[0])
	}
}

func TestImport_AttributeGroupValidation(t *testing.T) {
	csv := `name,slug,sku,color,size
Widget,widget,SKU-001,Red,Large
`
	reg := catalog.NewAttributeRegistry()
	colorAttr, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	sizeAttr, _ := catalog.NewAttribute("size", "Size", catalog.AttributeTypeText)
	reg.RegisterAttribute(colorAttr)
	reg.RegisterAttribute(sizeAttr)
	_ = reg.RegisterGroup(catalog.AttributeGroup{Code: "apparel", Label: "Apparel", Attributes: []string{"color", "size"}})

	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	imp.WithAttributeValidation(reg, "apparel")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", result.Errors)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
	attrs := varRepo.variants[0].Attributes
	if attrs["color"] != "Red" || attrs["size"] != "Large" {
		t.Errorf("attrs = %v, want color=Red size=Large", attrs)
	}
}

func TestImport_AttributeUndeclaredKeyInGroup(t *testing.T) {
	csv := `name,slug,sku,color,extra
Widget,widget,SKU-001,Red,Surprise
`
	reg := catalog.NewAttributeRegistry()
	colorAttr, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	reg.RegisterAttribute(colorAttr)
	_ = reg.RegisterGroup(catalog.AttributeGroup{Code: "simple", Label: "Simple", Attributes: []string{"color"}})

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)
	imp.WithAttributeValidation(reg, "simple")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected errors for undeclared attribute 'extra'")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "extra") && strings.Contains(e, "not declared") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %v, want one mentioning 'extra' and 'not declared'", result.Errors)
	}
}

func TestImport_AttributeRequiredMissing(t *testing.T) {
	csv := `name,slug,sku,color
Widget,widget,SKU-001,
`
	reg := catalog.NewAttributeRegistry()
	colorAttr, _ := catalog.NewAttribute("color", "Color", catalog.AttributeTypeText)
	colorAttr.Required = true
	reg.RegisterAttribute(colorAttr)
	_ = reg.RegisterGroup(catalog.AttributeGroup{Code: "colors", Label: "Colors", Attributes: []string{"color"}})

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)
	imp.WithAttributeValidation(reg, "colors")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected errors for missing required attribute 'color'")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "color") && strings.Contains(e, "required") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %v, want one mentioning 'color' and 'required'", result.Errors)
	}
}

func TestImport_AttributeSelectValidation(t *testing.T) {
	csv := `name,slug,sku,size
Widget,widget,SKU-001,XL
`
	reg := catalog.NewAttributeRegistry()
	sizeAttr, _ := catalog.NewAttribute("size", "Size", catalog.AttributeTypeSelect)
	sizeAttr.Options = []string{"S", "M", "L"}
	reg.RegisterAttribute(sizeAttr)
	_ = reg.RegisterGroup(catalog.AttributeGroup{Code: "sizing", Label: "Sizing", Attributes: []string{"size"}})

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)
	imp.WithAttributeValidation(reg, "sizing")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected errors for invalid select option 'XL'")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "size") && strings.Contains(e, "not in allowed options") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %v, want one mentioning 'size' and 'not in allowed options'", result.Errors)
	}
}

func TestImport_AttributeEmptyValuesOmitted(t *testing.T) {
	csv := `name,slug,sku,color,weight
Widget,widget,SKU-001,Red,
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", result.Errors)
	}
	attrs := varRepo.variants[0].Attributes
	if attrs["color"] != "Red" {
		t.Errorf("color = %v, want Red", attrs["color"])
	}
	if _, ok := attrs["weight"]; ok {
		t.Errorf("weight should be omitted for empty value, got %v", attrs["weight"])
	}
}

func TestImport_AttributeUnknownAttrNoGroupFallsBackToString(t *testing.T) {
	csv := `name,slug,sku,custom_field
Widget,widget,SKU-001,freeform
`
	reg := catalog.NewAttributeRegistry()

	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want none", result.Errors)
	}
	attrs := varRepo.variants[0].Attributes
	if attrs["custom_field"] != "freeform" {
		t.Errorf("custom_field = %v, want freeform (string fallback)", attrs["custom_field"])
	}
}

func TestImport_AttributeErrorSkipsRowNotGroup(t *testing.T) {
	csv := `name,slug,sku,weight
Widget,widget,SKU-001,notanumber
Widget,widget,SKU-002,3.5
`
	reg := catalog.NewAttributeRegistry()
	attr, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	reg.RegisterAttribute(attr)

	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (only the bad row)", result.Skipped)
	}
	if result.Variants != 1 {
		t.Errorf("Variants = %d, want 1 (the valid sibling)", result.Variants)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("len(Errors) = %d, want 1", len(result.Errors))
	}
}

func TestImport_SelectValidatedWithoutGroup(t *testing.T) {
	csv := `name,slug,sku,size
Widget,widget,SKU-001,XL
`
	reg := catalog.NewAttributeRegistry()
	attr, _ := catalog.NewAttribute("size", "Size", catalog.AttributeTypeSelect)
	attr.Options = []string{"S", "M", "L"}
	reg.RegisterAttribute(attr)

	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{}, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) == 0 {
		t.Fatal("expected errors for invalid select option without group")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "size") && strings.Contains(e, "not in allowed options") {
			found = true
		}
	}
	if !found {
		t.Errorf("errors = %v, want one mentioning 'size' and 'not in allowed options'", result.Errors)
	}
}

func TestImport_AllRowsFailNoProductCreated(t *testing.T) {
	csv := `name,slug,sku,weight
Widget,widget,SKU-001,bad1
Widget,widget,SKU-002,bad2
`
	reg := catalog.NewAttributeRegistry()
	wAttr, _ := catalog.NewAttribute("weight", "Weight", catalog.AttributeTypeNumber)
	reg.RegisterAttribute(wAttr)

	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	imp.WithAttributeValidation(reg, "")

	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 {
		t.Errorf("Products = %d, want 0 (all rows failed)", result.Products)
	}
	if result.Variants != 0 {
		t.Errorf("Variants = %d, want 0", result.Variants)
	}
	if result.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", result.Skipped)
	}
	if len(prodRepo.products) != 0 {
		t.Errorf("products created = %d, want 0", len(prodRepo.products))
	}
}

func TestImport_CreateBundleModes(t *testing.T) {
	csv := `name,slug,sku,type,pricing_mode,stock_mode,explode_order_lines
Kit,kit,SKU-K,bundle,dynamic,dynamic,true
Plain,plain,SKU-P,simple,,,
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 2 || len(result.Errors) != 0 {
		t.Fatalf("result = %+v", result)
	}
	bySlug := map[string]*catalog.Product{}
	for _, p := range prodRepo.products {
		bySlug[p.Slug] = p
	}
	kit := bySlug["kit"]
	if kit.PricingMode != catalog.PricingModeDynamic || kit.StockMode != catalog.StockModeDynamic || !kit.ExplodeOrderLines {
		t.Fatalf("kit modes = %s %s explode=%v", kit.PricingMode, kit.StockMode, kit.ExplodeOrderLines)
	}
	plain := bySlug["plain"]
	if plain.PricingMode != catalog.PricingModeFixed || plain.StockMode != catalog.StockModeFixed || plain.ExplodeOrderLines {
		t.Fatalf("plain modes = %s %s explode=%v", plain.PricingMode, plain.StockMode, plain.ExplodeOrderLines)
	}
}

func TestImport_DynamicModeOnSimpleIsRejected(t *testing.T) {
	csv := `name,slug,sku,type,pricing_mode
Widget,widget,SKU-1,simple,dynamic
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || len(prodRepo.products) != 0 {
		t.Fatalf("want no create, result=%+v", result)
	}
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0], "line 2:") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestImport_ExistingModeMismatchSkips(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Type: catalog.TypeBundle,
		PricingMode: catalog.PricingModeFixed, StockMode: catalog.StockModeFixed,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "kit" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,pricing_mode
Kit,kit,SKU-1,bundle,dynamic
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("variants created on mode mismatch: %+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "pricing_mode updates via CSV are not supported") || !strings.Contains(joined, "line 2:") {
		t.Fatalf("errors = %v", result.Errors)
	}
	if existing.PricingMode != catalog.PricingModeFixed {
		t.Fatalf("stored mode changed to %s", existing.PricingMode)
	}
}

func TestImport_ExistingEmptyModeColumnsDoNotConflict(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Type: catalog.TypeBundle,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic, ExplodeOrderLines: true,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "kit" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type
Kit,kit,SKU-NEW,bundle
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 || result.Variants != 1 {
		t.Fatalf("result = %+v", result)
	}
	if existing.PricingMode != catalog.PricingModeDynamic || !existing.ExplodeOrderLines {
		t.Fatalf("stored modes changed: %+v", existing)
	}
}

func TestImport_ExplodeTrueWithFixedModesRejected(t *testing.T) {
	csv := `name,slug,sku,type,pricing_mode,stock_mode,explode_order_lines
Kit,kit,SKU-K,bundle,fixed,fixed,true
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || len(prodRepo.products) != 0 {
		t.Fatalf("want no create, result=%+v products=%d", result, len(prodRepo.products))
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "line 2:") || !strings.Contains(joined, "explode order lines") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestImport_SlugGroupModeConflict(t *testing.T) {
	csv := `name,slug,sku,type,pricing_mode,stock_mode,explode_order_lines
Kit,kit,SKU-1,bundle,fixed,fixed,false
Kit,kit,SKU-2,bundle,dynamic,fixed,false
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || len(prodRepo.products) != 0 || result.Variants != 0 {
		t.Fatalf("want no create, result=%+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "conflicting pricing_mode") || !strings.Contains(joined, "line 2:") || !strings.Contains(joined, "line 3:") {
		t.Fatalf("errors = %v", result.Errors)
	}
}

func TestImport_MatchingModeReimportSucceeds(t *testing.T) {
	existing := &catalog.Product{
		ID: "p1", Name: "Kit", Slug: "kit", Type: catalog.TypeBundle,
		PricingMode: catalog.PricingModeDynamic, StockMode: catalog.StockModeDynamic, ExplodeOrderLines: true,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(_ context.Context, slug string) (*catalog.Product, error) {
			if slug == "kit" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,pricing_mode,stock_mode,explode_order_lines
Kit,kit,SKU-NEW,bundle,dynamic,dynamic,true
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 || result.Variants != 1 || len(varRepo.variants) != 1 {
		t.Fatalf("result=%+v variants=%d", result, len(varRepo.variants))
	}
	if existing.PricingMode != catalog.PricingModeDynamic || existing.StockMode != catalog.StockModeDynamic || !existing.ExplodeOrderLines {
		t.Fatalf("stored modes changed: %+v", existing)
	}
}

func TestImport_ModeColumnsAreNotAttributes(t *testing.T) {
	csv := `name,slug,sku,type,pricing_mode,color
Widget,widget,SKU-1,simple,fixed,red
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 || len(varRepo.variants) != 1 {
		t.Fatalf("result=%+v variants=%d", result, len(varRepo.variants))
	}
	attrs := varRepo.variants[0].Attributes
	if attrs["color"] != "red" {
		t.Fatalf("color = %#v", attrs["color"])
	}
	if _, ok := attrs["pricing_mode"]; ok {
		t.Fatalf("pricing_mode stored as attribute: %#v", attrs)
	}
}

func TestImport_RequiresLinkedParentColumnIsNotAttribute(t *testing.T) {
	csv := `name,slug,sku,type,requires_linked_parent,color
Ticket,ticket,SKU-1,simple,true,red
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Errors) != 0 || len(varRepo.variants) != 1 {
		t.Fatalf("result=%+v variants=%d", result, len(varRepo.variants))
	}
	if len(prodRepo.products) != 1 || !prodRepo.products[0].RequiresLinkedParent {
		t.Fatalf("product flag = %+v, want RequiresLinkedParent true", prodRepo.products)
	}
	attrs := varRepo.variants[0].Attributes
	if attrs["color"] != "red" {
		t.Fatalf("color = %#v", attrs["color"])
	}
	if _, ok := attrs["requires_linked_parent"]; ok {
		t.Fatalf("requires_linked_parent stored as attribute: %#v", attrs)
	}
}

func TestImport_InvalidModeFieldsSkipRowOnce(t *testing.T) {
	csv := `name,slug,sku,type,pricing_mode,stock_mode,explode_order_lines
Widget,widget,SKU-1,nope,bogus,nope,maybe
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 0 || result.Variants != 0 || len(prodRepo.products) != 0 {
		t.Fatalf("want no create, result=%+v", result)
	}
	if result.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1; errors=%v", result.Skipped, result.Errors)
	}
	if len(result.Errors) != 4 {
		t.Fatalf("len(Errors) = %d, want 4 (type, pricing_mode, stock_mode, explode); errors=%v", len(result.Errors), result.Errors)
	}
	for _, errText := range result.Errors {
		if !strings.Contains(errText, "line 2:") {
			t.Fatalf("error %q, want line 2", errText)
		}
	}
}

func TestImport_RequiresLinkedParentMismatchOnExisting(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Ticket", Slug: "ticket", Type: catalog.TypeSimple,
		RequiresLinkedParent: false,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "ticket" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,requires_linked_parent
Ticket,ticket,SKU-1,simple,true
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped < 1 {
		t.Fatalf("Skipped = %d, want >=1; errors=%v", result.Skipped, result.Errors)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "requires_linked_parent updates via CSV are not supported") {
		t.Fatalf("Errors = %v", result.Errors)
	}
	if existing.RequiresLinkedParent {
		t.Fatal("existing product flag must not flip")
	}
}

func TestImport_RequiresLinkedParentConflictWithinSlug(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,requires_linked_parent
Ticket,ticket,SKU-1,simple,true
Ticket,ticket,SKU-2,simple,false
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(prodRepo.products) != 0 {
		t.Fatalf("want no create on conflict, got %+v", prodRepo.products)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "conflicting requires_linked_parent") {
		t.Fatalf("Errors = %v", result.Errors)
	}
}

func TestImport_VisibilityModesCreate(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,visibility_catalog,visibility_search,visibility_individually,visibility_purchasable
Hidden,hidden,SKU-H,simple,hidden,visible,auto,hidden
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || len(prodRepo.products) != 1 {
		t.Fatalf("result=%+v products=%d", result, len(prodRepo.products))
	}
	want := catalog.VisibilityAxes{
		Catalog:      catalog.VisibilityModeHidden,
		Search:       catalog.VisibilityModeVisible,
		Individually: catalog.VisibilityModeAuto,
		Purchasable:  catalog.VisibilityModeHidden,
	}
	if prodRepo.products[0].VisibilityModes != want {
		t.Fatalf("VisibilityModes = %#v, want %#v", prodRepo.products[0].VisibilityModes, want)
	}
}

func TestImport_VisibilityModesEmptyDefaultsAuto(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type
Plain,plain,SKU-P,simple
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || len(prodRepo.products) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if prodRepo.products[0].VisibilityModes != catalog.DefaultVisibilityAxes() {
		t.Fatalf("VisibilityModes = %#v, want defaults", prodRepo.products[0].VisibilityModes)
	}
}

func TestImport_VisibilityModesMismatchOnExisting(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,visibility_catalog
Widget,widget,SKU-1,simple,hidden
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped < 1 {
		t.Fatalf("Skipped = %d, want >=1; errors=%v", result.Skipped, result.Errors)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "visibility_catalog updates via CSV are not supported") {
		t.Fatalf("Errors = %v", result.Errors)
	}
	if existing.VisibilityModes.Catalog != catalog.VisibilityModeAuto {
		t.Fatal("existing visibility must not flip")
	}
}

func TestImport_VisibilityModesInvalid(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,visibility_search
Bad,bad,SKU-B,forced
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(prodRepo.products) != 0 {
		t.Fatalf("want no create, got %+v", prodRepo.products)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "invalid visibility mode") {
		t.Fatalf("Errors = %v", result.Errors)
	}
}

func TestImport_VisibilityColumnsAreNotAttributes(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,visibility_catalog,color
P,p,SKU-1,hidden,red
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || len(varRepo.variants) != 1 {
		t.Fatalf("result=%+v variants=%d", result, len(varRepo.variants))
	}
	if _, ok := varRepo.variants[0].Attributes["visibility_catalog"]; ok {
		t.Fatal("visibility_catalog must not be stored as attribute")
	}
	if varRepo.variants[0].Attributes["color"] != "red" {
		t.Fatalf("color = %v", varRepo.variants[0].Attributes["color"])
	}
	if prodRepo.products[0].VisibilityModes.Catalog != catalog.VisibilityModeHidden {
		t.Fatalf("catalog mode = %q", prodRepo.products[0].VisibilityModes.Catalog)
	}
}

func TestImport_VisibilityModesMismatchSkipsVariants(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple,
		VisibilityModes: catalog.DefaultVisibilityAxes(),
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,visibility_catalog
Widget,widget,SKU-NEW,simple,hidden
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want no variants on visibility mismatch, got result=%+v variants=%d", result, len(varRepo.variants))
	}
}

func TestImport_VisibilityModesConflictInSlugGroup(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,visibility_catalog
Widget,widget,SKU-1,hidden
Widget,widget,SKU-2,visible
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(prodRepo.products) != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want no create on conflict, products=%d variants=%d", len(prodRepo.products), len(varRepo.variants))
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "conflicting visibility_catalog") {
		t.Fatalf("Errors = %v", result.Errors)
	}
}

func TestImport_ExistingProductEmptyVisibilityColumnsAddsVariants(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple,
		VisibilityModes: catalog.VisibilityAxes{
			Catalog: catalog.VisibilityModeHidden, Search: catalog.VisibilityModeHidden,
			Individually: catalog.VisibilityModeHidden, Purchasable: catalog.VisibilityModeHidden,
		},
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	// Column present but empty — all-empty consensus skips mismatch check.
	csv := `name,slug,sku,type,visibility_catalog
Widget,widget,SKU-NEW,simple,
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 || len(varRepo.variants) != 1 {
		t.Fatalf("want variant add with omitted/empty visibility, got result=%+v", result)
	}
	if existing.VisibilityModes.Catalog != catalog.VisibilityModeHidden {
		t.Fatal("stored visibility must not flip when CSV column empty")
	}
}

func TestImport_ExistingProductOmittedVisibilityColumnsAddsVariants(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple,
		VisibilityModes: catalog.VisibilityAxes{
			Catalog: catalog.VisibilityModeVisible, Search: catalog.VisibilityModeAuto,
			Individually: catalog.VisibilityModeAuto, Purchasable: catalog.VisibilityModeAuto,
		},
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type
Widget,widget,SKU-NEW,simple
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1 (omitted visibility columns must not block)", result.Variants)
	}
}

func TestImport_ExistingProductMatchingVisibilityAddsVariant(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple,
		VisibilityModes: catalog.VisibilityAxes{
			Catalog: catalog.VisibilityModeHidden, Search: catalog.VisibilityModeAuto,
			Individually: catalog.VisibilityModeAuto, Purchasable: catalog.VisibilityModeAuto,
		},
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,visibility_catalog
Widget,widget,SKU-NEW,simple,hidden
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
}

func TestImport_VisibilityModesCaseSensitiveLikeType(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,visibility_catalog
Bad,bad,SKU-B,HIDDEN
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(prodRepo.products) != 0 {
		t.Fatalf("want reject HIDDEN (case-sensitive), got %+v", prodRepo.products)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "invalid visibility mode") {
		t.Fatalf("Errors = %v", result.Errors)
	}
}

func TestImport_StatusCreate(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,status
Live,live,SKU-L,simple,active
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || len(prodRepo.products) != 1 {
		t.Fatalf("result=%+v", result)
	}
	if prodRepo.products[0].Status != catalog.StatusActive {
		t.Fatalf("Status = %q, want active", prodRepo.products[0].Status)
	}
}

func TestImport_StatusEmptyDefaultsDraft(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type
Plain,plain,SKU-P,simple
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 1 || prodRepo.products[0].Status != catalog.StatusDraft {
		t.Fatalf("Status = %q, want draft; result=%+v", prodRepo.products[0].Status, result)
	}
}

func TestImport_StatusMismatchOnExistingSkipsVariants(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple, Status: catalog.StatusDraft,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,status
Widget,widget,SKU-NEW,simple,active
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want no variants on status mismatch, got %+v", result)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "status updates via CSV are not supported") {
		t.Fatalf("Errors = %v", result.Errors)
	}
}

func TestImport_ExistingProductEmptyStatusColumnAddsVariants(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple, Status: catalog.StatusActive,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,status
Widget,widget,SKU-NEW,simple,
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
	if existing.Status != catalog.StatusActive {
		t.Fatalf("stored status mutated to %q", existing.Status)
	}
}

func TestImport_StatusInvalid(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,status
Bad,bad,SKU-B,ACTIVE
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(prodRepo.products) != 0 {
		t.Fatalf("want reject ACTIVE, got %+v", prodRepo.products)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "invalid product status") {
		t.Fatalf("Errors = %v", result.Errors)
	}
}

func TestImport_StatusConflictInSlugGroup(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,status
Widget,widget,SKU-1,draft
Widget,widget,SKU-2,active
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "conflicting status") {
		t.Fatalf("Errors = %v", result.Errors)
	}
	if result.Products != 0 || len(prodRepo.products) != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want create nothing on conflict, result=%+v products=%d variants=%d",
			result, len(prodRepo.products), len(varRepo.variants))
	}
}

func TestImport_StatusEmptyPlusActiveConflictInSlugGroup(t *testing.T) {
	// Empty cells count as draft when any status is set — same as type consensus.
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,status
Widget,widget,SKU-1,
Widget,widget,SKU-2,active
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, "conflicting status") {
		t.Fatalf("Errors = %v, want empty+active conflict (empty counts as draft)", result.Errors)
	}
	if len(prodRepo.products) != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want create nothing on conflict, got products=%d variants=%d", len(prodRepo.products), len(varRepo.variants))
	}
}

func TestImport_ExistingProductMatchingStatusAddsVariant(t *testing.T) {
	existing := &catalog.Product{
		ID: "prod-1", Name: "Widget", Slug: "widget", Type: catalog.TypeSimple, Status: catalog.StatusActive,
	}
	prodRepo := &mockProductRepo{
		findBySlugFn: func(ctx context.Context, slug string) (*catalog.Product, error) {
			if slug == "widget" {
				return existing, nil
			}
			return nil, nil
		},
	}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,status
Widget,widget,SKU-NEW,simple,active
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Variants != 1 {
		t.Fatalf("Variants = %d, want 1", result.Variants)
	}
	if existing.Status != catalog.StatusActive {
		t.Fatalf("stored status mutated to %q", existing.Status)
	}
}

func TestImport_DownloadableActiveRejectedCreatesNothing(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,status
Ebook,ebook,SKU-DL,downloadable,active
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(prodRepo.products) != 0 || len(varRepo.variants) != 0 {
		t.Fatalf("want create nothing for downloadable+active, got products=%d variants=%d", len(prodRepo.products), len(varRepo.variants))
	}
	joined := strings.Join(result.Errors, "\n")
	if !strings.Contains(joined, catalog.DownloadableRequiresFileMessage) {
		t.Fatalf("Errors = %v, want %q", result.Errors, catalog.DownloadableRequiresFileMessage)
	}
}

func TestImport_ActiveSimpleVirtualBundleStillCreateActive(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo, nil)
	csv := `name,slug,sku,type,status,pricing_mode,stock_mode
Card,card,SKU-CARD,simple,active,,
Ticket,ticket,SKU-TICKET,virtual,active,,
Kit,kit,SKU-KIT,bundle,active,dynamic,dynamic
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 3 || len(result.Errors) != 0 {
		t.Fatalf("result=%+v", result)
	}
	bySlug := map[string]*catalog.Product{}
	for _, p := range prodRepo.products {
		bySlug[p.Slug] = p
	}
	for _, slug := range []string{"card", "ticket", "kit"} {
		p := bySlug[slug]
		if p == nil || p.Status != catalog.StatusActive {
			t.Fatalf("%s = %#v, want StatusActive", slug, p)
		}
	}
}

func TestImport_PublishesEventProductCreated(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	bus := event.NewBus(&noopLogger{})
	var got []catalog.ProductCreatedData
	bus.On(catalog.EventProductCreated, func(_ context.Context, evt event.Event) error {
		d, ok := evt.Data.(catalog.ProductCreatedData)
		if !ok {
			t.Errorf("unexpected data type %T", evt.Data)
			return nil
		}
		got = append(got, d)
		return nil
	})
	imp := importer.NewProductImporter(prodRepo, varRepo, nil).WithBus(bus)
	csv := `name,slug,sku,type,status
Live,live,SKU-L,simple,active
Draft,drafty,SKU-D,simple,draft
`
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Products != 2 {
		t.Fatalf("Products = %d, want 2", result.Products)
	}
	if len(got) != 2 {
		t.Fatalf("EventProductCreated count = %d, want 2 (active create must not be silent on the bus)", len(got))
	}
	bySlug := map[string]catalog.ProductCreatedData{}
	for _, d := range got {
		bySlug[d.Slug] = d
	}
	if bySlug["live"].Status != catalog.StatusActive || bySlug["live"].ProductID == "" {
		t.Fatalf("live event = %#v", bySlug["live"])
	}
	if bySlug["drafty"].Status != catalog.StatusDraft {
		t.Fatalf("drafty event = %#v", bySlug["drafty"])
	}
}

