package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"github.com/akarso/shopanda/internal/testutil"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
)

// --- mocks ---

type mockProductRepo struct {
	products []catalog.Product
	listErr  error
}

func (m *mockProductRepo) FindByID(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) FindBySlug(_ context.Context, _ string) (*catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) List(_ context.Context, filter catalog.ListFilter) ([]catalog.Product, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if filter.Offset >= len(m.products) {
		return nil, nil
	}
	end := filter.Offset + filter.Limit
	if end > len(m.products) {
		end = len(m.products)
	}
	return m.products[filter.Offset:end], nil
}
func (m *mockProductRepo) FindByCategoryID(_ context.Context, _ string, _, _ int) ([]catalog.Product, error) {
	return nil, nil
}
func (m *mockProductRepo) Create(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockProductRepo) Update(_ context.Context, _ *catalog.Product) error { return nil }
func (m *mockProductRepo) WithTx(_ *sql.Tx) catalog.ProductRepository         { return m }

type mockVariantRepo struct {
	variants map[string][]catalog.Variant // keyed by product ID
	listErr  error
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
func (m *mockVariantRepo) ListByProductID(_ context.Context, productID string, offset, limit int) ([]catalog.Variant, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	all := m.variants[productID]
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}
func (m *mockVariantRepo) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, m.ListByProductID, productIDs, limitPerProduct)
}

func (m *mockVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository         { return m }

// --- helpers ---

func parseCSV(t *testing.T, buf *bytes.Buffer) [][]string {
	t.Helper()
	r := csv.NewReader(buf)
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("parse CSV output: %v", err)
	}
	return records
}

// --- tests ---

func TestExport_BasicCSV(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget", Description: "A fine widget"},
			{ID: "p2", Name: "Gadget", Slug: "gadget", Description: "A cool gadget"},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {
				{ID: "v1", ProductID: "p1", SKU: "SKU-001", Name: "Size S"},
				{ID: "v2", ProductID: "p1", SKU: "SKU-002", Name: "Size M"},
			},
			"p2": {
				{ID: "v3", ProductID: "p2", SKU: "SKU-003", Name: "Default"},
			},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Products != 2 {
		t.Errorf("Products = %d, want 2", result.Products)
	}
	if result.Variants != 3 {
		t.Errorf("Variants = %d, want 3", result.Variants)
	}

	records := parseCSV(t, &buf)
	if len(records) != 4 { // 1 header + 3 data rows
		t.Fatalf("rows = %d, want 4", len(records))
	}
	header := records[0]
	if strings.Join(header, ",") != "name,slug,sku,description,type,variant_name" {
		t.Errorf("header = %v, want [name slug sku description type variant_name]", header)
	}
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}
	for _, row := range records[1:] {
		if row[colIdx["type"]] != "simple" {
			t.Errorf("row type = %q, want simple for empty Product.Type", row[colIdx["type"]])
		}
	}
}

func TestExport_WithAttributes(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget"},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {
				{ID: "v1", ProductID: "p1", SKU: "SKU-001", Attributes: map[string]interface{}{
					"color":  "Red",
					"weight": 2.5,
					"active": true,
				}},
			},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Variants != 1 {
		t.Errorf("Variants = %d, want 1", result.Variants)
	}

	records := parseCSV(t, &buf)
	header := records[0]
	// Attribute columns should be sorted: active, color, weight
	expected := "name,slug,sku,description,type,variant_name,active,color,weight"
	if strings.Join(header, ",") != expected {
		t.Errorf("header = %q, want %q", strings.Join(header, ","), expected)
	}
	data := records[1]
	// Find column indices
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}
	if data[colIdx["color"]] != "Red" {
		t.Errorf("color = %q, want Red", data[colIdx["color"]])
	}
	if data[colIdx["weight"]] != "2.5" {
		t.Errorf("weight = %q, want 2.5", data[colIdx["weight"]])
	}
	if data[colIdx["active"]] != "true" {
		t.Errorf("active = %q, want true", data[colIdx["active"]])
	}
}

func TestExport_EmptyDatabase(t *testing.T) {
	prodRepo := &mockProductRepo{}
	varRepo := &mockVariantRepo{variants: map[string][]catalog.Variant{}}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Products != 0 || result.Variants != 0 {
		t.Errorf("expected all zeros, got products=%d variants=%d", result.Products, result.Variants)
	}
	records := parseCSV(t, &buf)
	if len(records) != 1 { // header only
		t.Errorf("rows = %d, want 1 (header only)", len(records))
	}
	if strings.Join(records[0], ",") != "name,slug,sku,description,type,variant_name" {
		t.Errorf("header = %v, want type column present", records[0])
	}
}

func TestExport_IncludesType(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "E-book", Slug: "e-book", Type: catalog.TypeVirtual},
			{ID: "p2", Name: "Widget", Slug: "widget"}, // empty Type → export as simple
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{ID: "v1", ProductID: "p1", SKU: "SKU-EB"}},
			"p2": {{ID: "v2", ProductID: "p2", SKU: "SKU-W"}},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	records := parseCSV(t, &buf)
	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}
	if _, ok := colIdx["type"]; !ok {
		t.Fatal("type column missing from header")
	}
	bySKU := map[string][]string{}
	for _, row := range records[1:] {
		bySKU[row[colIdx["sku"]]] = row
	}
	if bySKU["SKU-EB"][colIdx["type"]] != "virtual" {
		t.Errorf("SKU-EB type = %q, want virtual", bySKU["SKU-EB"][colIdx["type"]])
	}
	if bySKU["SKU-W"][colIdx["type"]] != "simple" {
		t.Errorf("SKU-W type = %q, want simple", bySKU["SKU-W"][colIdx["type"]])
	}
}

func TestExport_AttributeNamedTypeDoesNotOverwriteProductType(t *testing.T) {
	// Reserved attribute key "type" is omitted from CSV (not emitted as a second
	// column); operators must rename such attributes to round-trip their values.
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "E-book", Slug: "e-book", Type: catalog.TypeVirtual},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{
				ID: "v1", ProductID: "p1", SKU: "SKU-EB",
				Attributes: map[string]interface{}{"type": "should-not-overwrite", "color": "red"},
			}},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	records := parseCSV(t, &buf)
	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}
	typeCount := 0
	for _, h := range header {
		if h == "type" {
			typeCount++
		}
	}
	if typeCount != 1 {
		t.Fatalf("header type occurrences = %d, want 1; header=%v", typeCount, header)
	}
	if records[1][colIdx["type"]] != "virtual" {
		t.Errorf("type cell = %q, want virtual (attribute must not overwrite)", records[1][colIdx["type"]])
	}
	if _, ok := colIdx["color"]; !ok {
		t.Fatal("color attribute column missing")
	}
	if records[1][colIdx["color"]] != "red" {
		t.Errorf("color = %q, want red", records[1][colIdx["color"]])
	}
	if len(result.Errors) != 0 {
		t.Fatalf("Errors = %v, want empty (omission must not fail export)", result.Errors)
	}
	omitted := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "omitted reserved attribute keys") && strings.Contains(w, "type") {
			omitted = true
			break
		}
	}
	if !omitted {
		t.Fatalf("Warnings = %v, want reserved-key omission note for type", result.Warnings)
	}
}

func TestExport_InvalidTypeEmittedAsIs(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Odd", Slug: "odd", Type: catalog.Type("kit")},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {{ID: "v1", ProductID: "p1", SKU: "SKU-ODD"}},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	records := parseCSV(t, &buf)
	colIdx := make(map[string]int)
	for i, h := range records[0] {
		colIdx[h] = i
	}
	if records[1][colIdx["type"]] != "kit" {
		t.Errorf("type = %q, want raw invalid value kit (not rewritten to simple)", records[1][colIdx["type"]])
	}
}

func TestExport_ProductWithNoVariants(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget"},
		},
	}
	varRepo := &mockVariantRepo{variants: map[string][]catalog.Variant{}}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Products != 0 {
		t.Errorf("Products = %d, want 0 (no variants means no rows)", result.Products)
	}
	if result.Variants != 0 {
		t.Errorf("Variants = %d, want 0", result.Variants)
	}
}

func TestExport_AttributeNilValuesAreEmpty(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget"},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {
				{ID: "v1", ProductID: "p1", SKU: "SKU-001", Attributes: map[string]interface{}{"color": "Red"}},
				{ID: "v2", ProductID: "p1", SKU: "SKU-002", Attributes: map[string]interface{}{"size": "L"}},
			},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	records := parseCSV(t, &buf)
	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}
	// v1 has color but no size → size should be empty
	if records[1][colIdx["size"]] != "" {
		t.Errorf("v1 size = %q, want empty", records[1][colIdx["size"]])
	}
	// v2 has size but no color → color should be empty
	if records[2][colIdx["color"]] != "" {
		t.Errorf("v2 color = %q, want empty", records[2][colIdx["color"]])
	}
}

func TestExport_ListProductsError(t *testing.T) {
	prodRepo := &mockProductRepo{listErr: fmt.Errorf("db unavailable")}
	varRepo := &mockVariantRepo{variants: map[string][]catalog.Variant{}}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error for failed product listing")
	}
	if !strings.Contains(err.Error(), "db unavailable") {
		t.Errorf("error = %q, want containing 'db unavailable'", err.Error())
	}
}

func TestExport_ListVariantsError(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget"},
		},
	}
	varRepo := &mockVariantRepo{listErr: fmt.Errorf("variant query failed")}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error for failed variant listing")
	}
	if !strings.Contains(err.Error(), "variant query failed") {
		t.Errorf("error = %q, want containing 'variant query failed'", err.Error())
	}
}

func TestExport_RoundTrip(t *testing.T) {
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget", Description: "Desc"},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{
			"p1": {
				{ID: "v1", ProductID: "p1", SKU: "SKU-001", Name: "Default", Attributes: map[string]interface{}{
					"color": "Blue",
				}},
			},
		},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	records := parseCSV(t, &buf)
	if len(records) != 2 {
		t.Fatalf("rows = %d, want 2", len(records))
	}
	header := records[0]
	row := records[1]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[h] = i
	}
	if row[colIdx["name"]] != "Widget" {
		t.Errorf("name = %q, want Widget", row[colIdx["name"]])
	}
	if row[colIdx["slug"]] != "widget" {
		t.Errorf("slug = %q, want widget", row[colIdx["slug"]])
	}
	if row[colIdx["sku"]] != "SKU-001" {
		t.Errorf("sku = %q, want SKU-001", row[colIdx["sku"]])
	}
	if row[colIdx["description"]] != "Desc" {
		t.Errorf("description = %q, want Desc", row[colIdx["description"]])
	}
	if row[colIdx["variant_name"]] != "Default" {
		t.Errorf("variant_name = %q, want Default", row[colIdx["variant_name"]])
	}
	if row[colIdx["color"]] != "Blue" {
		t.Errorf("color = %q, want Blue", row[colIdx["color"]])
	}
}

func TestExport_VariantPagination(t *testing.T) {
	// Create 150 variants for a single product to exceed pageSize (100).
	variants := make([]catalog.Variant, 150)
	for i := range variants {
		variants[i] = catalog.Variant{
			ID:        fmt.Sprintf("v%d", i+1),
			ProductID: "p1",
			SKU:       fmt.Sprintf("SKU-%03d", i+1),
			Name:      fmt.Sprintf("Variant %d", i+1),
		}
	}
	prodRepo := &mockProductRepo{
		products: []catalog.Product{
			{ID: "p1", Name: "Widget", Slug: "widget"},
		},
	}
	varRepo := &mockVariantRepo{
		variants: map[string][]catalog.Variant{"p1": variants},
	}

	exp := exporter.NewProductExporter(prodRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Products != 1 {
		t.Errorf("Products = %d, want 1", result.Products)
	}
	if result.Variants != 150 {
		t.Errorf("Variants = %d, want 150", result.Variants)
	}
	records := parseCSV(t, &buf)
	// 1 header + 150 data rows
	if len(records) != 151 {
		t.Fatalf("rows = %d, want 151", len(records))
	}
	// Verify first and last SKU to confirm all pages were traversed.
	if records[1][2] != "SKU-001" {
		t.Errorf("first SKU = %q, want SKU-001", records[1][2])
	}
	if records[150][2] != "SKU-150" {
		t.Errorf("last SKU = %q, want SKU-150", records[150][2])
	}
}
