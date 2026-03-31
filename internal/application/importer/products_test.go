package importer_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
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
func (m *mockProductRepo) List(_ context.Context, _, _ int) ([]catalog.Product, error) {
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
func (m *mockVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
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

// --- tests ---

func TestImport_BasicCSV(t *testing.T) {
	csv := `name,slug,sku,description,variant_name
Widget,widget,SKU-001,A fine widget,Size S
Widget,widget,SKU-002,,Size M
Gadget,gadget,SKU-003,A cool gadget,Default
`
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{}
	imp := importer.NewProductImporter(prodRepo, varRepo)

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
	if prodRepo.products[0].Name != "Widget" {
		t.Errorf("product[0].Name = %q, want Widget", prodRepo.products[0].Name)
	}
	if prodRepo.products[0].Slug != "widget" {
		t.Errorf("product[0].Slug = %q, want widget", prodRepo.products[0].Slug)
	}
	if prodRepo.products[0].Description != "A fine widget" {
		t.Errorf("product[0].Description = %q, want 'A fine widget'", prodRepo.products[0].Description)
	}

	// Check variants.
	if len(varRepo.variants) != 3 {
		t.Fatalf("variants created = %d, want 3", len(varRepo.variants))
	}
	// First two variants belong to same product.
	if varRepo.variants[0].ProductID != varRepo.variants[1].ProductID {
		t.Error("first two variants should share product ID")
	}
	// Third variant is a different product.
	if varRepo.variants[2].ProductID == varRepo.variants[0].ProductID {
		t.Error("third variant should have a different product ID")
	}
	if varRepo.variants[0].SKU != "SKU-001" {
		t.Errorf("variant[0].SKU = %q, want SKU-001", varRepo.variants[0].SKU)
	}
	if varRepo.variants[0].Name != "Size S" {
		t.Errorf("variant[0].Name = %q, want 'Size S'", varRepo.variants[0].Name)
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
	imp := importer.NewProductImporter(prodRepo, varRepo)

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
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{})

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
	imp := importer.NewProductImporter(prodRepo, varRepo)

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
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{})

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
	imp := importer.NewProductImporter(prodRepo, varRepo)

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
	imp := importer.NewProductImporter(prodRepo, varRepo)

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
	imp := importer.NewProductImporter(&mockProductRepo{}, &mockVariantRepo{})

	_, err := imp.Import(context.Background(), strings.NewReader(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}
