package importer_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
)

// --- stock test mocks ---

type mockVariantRepoForStock struct {
	variants map[string]*catalog.Variant // keyed by SKU
}

func (m *mockVariantRepoForStock) FindByID(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForStock) FindBySKU(_ context.Context, sku string) (*catalog.Variant, error) {
	return m.variants[sku], nil
}
func (m *mockVariantRepoForStock) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForStock) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepoForStock) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepoForStock) WithTx(_ *sql.Tx) catalog.VariantRepository {
	return m
}

type mockStockRepo struct {
	entries map[string]int // variantID → quantity
	setErr  error
}

func newMockStockRepo() *mockStockRepo {
	return &mockStockRepo{entries: make(map[string]int)}
}

func (m *mockStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	qty, ok := m.entries[variantID]
	if !ok {
		return inventory.StockEntry{VariantID: variantID, Quantity: 0}, nil
	}
	return inventory.StockEntry{VariantID: variantID, Quantity: qty}, nil
}

func (m *mockStockRepo) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	if m.setErr != nil {
		return m.setErr
	}
	m.entries[entry.VariantID] = entry.Quantity
	return nil
}

func (m *mockStockRepo) ListStock(_ context.Context, offset, limit int) ([]inventory.StockEntry, error) {
	return nil, nil
}

// --- tests ---

func TestStockImport_Basic(t *testing.T) {
	varRepo := &mockVariantRepoForStock{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
			"SKU-002": {ID: "v2", SKU: "SKU-002"},
		},
	}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,10\nSKU-002,25\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 2 {
		t.Errorf("Updated = %d, want 2", result.Updated)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
	if stockRepo.entries["v1"] != 10 {
		t.Errorf("v1 stock = %d, want 10", stockRepo.entries["v1"])
	}
	if stockRepo.entries["v2"] != 25 {
		t.Errorf("v2 stock = %d, want 25", stockRepo.entries["v2"])
	}
}

func TestStockImport_MissingColumns(t *testing.T) {
	varRepo := &mockVariantRepoForStock{variants: map[string]*catalog.Variant{}}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,name\nSKU-001,Widget\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for missing quantity column")
	}
	if !strings.Contains(err.Error(), "quantity") {
		t.Errorf("error = %q, want containing 'quantity'", err.Error())
	}
}

func TestStockImport_UnknownSKU(t *testing.T) {
	varRepo := &mockVariantRepoForStock{variants: map[string]*catalog.Variant{}}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nNOSUCH,10\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0", result.Updated)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "unknown sku") {
		t.Errorf("error = %q, want containing 'unknown sku'", result.Errors[0])
	}
}

func TestStockImport_InvalidQuantity(t *testing.T) {
	varRepo := &mockVariantRepoForStock{variants: map[string]*catalog.Variant{
		"SKU-001": {ID: "v1", SKU: "SKU-001"},
	}}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,abc\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0", result.Updated)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestStockImport_NegativeQuantity(t *testing.T) {
	varRepo := &mockVariantRepoForStock{variants: map[string]*catalog.Variant{
		"SKU-001": {ID: "v1", SKU: "SKU-001"},
	}}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,-5\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "negative quantity") {
		t.Errorf("error = %q, want containing 'negative quantity'", result.Errors[0])
	}
}

func TestStockImport_EmptySKU(t *testing.T) {
	varRepo := &mockVariantRepoForStock{variants: map[string]*catalog.Variant{}}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\n,10\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "empty sku") {
		t.Errorf("error = %q, want containing 'empty sku'", result.Errors[0])
	}
}

func TestStockImport_SetStockError(t *testing.T) {
	varRepo := &mockVariantRepoForStock{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
		},
	}
	stockRepo := newMockStockRepo()
	stockRepo.setErr = fmt.Errorf("db write failed")
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,10\n"
	_, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err == nil {
		t.Fatal("expected error for SetStock failure")
	}
	if !strings.Contains(err.Error(), "db write failed") {
		t.Errorf("error = %q, want containing 'db write failed'", err.Error())
	}
}

func TestStockImport_ZeroQuantity(t *testing.T) {
	varRepo := &mockVariantRepoForStock{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
		},
	}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,0\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 1 {
		t.Errorf("Updated = %d, want 1", result.Updated)
	}
	if stockRepo.entries["v1"] != 0 {
		t.Errorf("v1 stock = %d, want 0", stockRepo.entries["v1"])
	}
}

func TestStockImport_MixedValidAndInvalid(t *testing.T) {
	varRepo := &mockVariantRepoForStock{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
			"SKU-002": {ID: "v2", SKU: "SKU-002"},
		},
	}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,10\nBAD,5\nSKU-002,abc\nSKU-002,20\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 2 {
		t.Errorf("Updated = %d, want 2", result.Updated)
	}
	if result.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", result.Skipped)
	}
	if len(result.Errors) != 2 {
		t.Errorf("Errors = %d, want 2", len(result.Errors))
	}
}
