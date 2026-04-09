package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
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
func (m *mockProductRepo) List(_ context.Context, offset, limit int) ([]catalog.Product, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if offset >= len(m.products) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.products) {
		end = len(m.products)
	}
	return m.products[offset:end], nil
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
func (m *mockVariantRepo) ListByProductID(_ context.Context, productID string, _, _ int) ([]catalog.Variant, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.variants[productID], nil
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
	if strings.Join(header, ",") != "name,slug,sku,description,variant_name" {
		t.Errorf("header = %v, want [name slug sku description variant_name]", header)
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
	expected := "name,slug,sku,description,variant_name,active,color,weight"
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
