package importer_test

import (
	"context"
	"database/sql"
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
func (m *mockVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository { return m }

// --- tests ---

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
