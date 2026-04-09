package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
)

// --- stock test mocks ---

type mockStockRepo struct {
	entries []inventory.StockEntry
	listErr error
}

func (m *mockStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	for _, e := range m.entries {
		if e.VariantID == variantID {
			return e, nil
		}
	}
	return inventory.StockEntry{VariantID: variantID, Quantity: 0}, nil
}

func (m *mockStockRepo) SetStock(_ context.Context, _ *inventory.StockEntry) error {
	return nil
}

func (m *mockStockRepo) ListStock(_ context.Context, offset, limit int) ([]inventory.StockEntry, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if offset >= len(m.entries) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.entries) {
		end = len(m.entries)
	}
	return m.entries[offset:end], nil
}

type mockVariantRepoForStockExport struct {
	variants map[string]*catalog.Variant // keyed by ID
}

func (m *mockVariantRepoForStockExport) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return m.variants[id], nil
}
func (m *mockVariantRepoForStockExport) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForStockExport) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForStockExport) Create(_ context.Context, _ *catalog.Variant) error {
	return nil
}
func (m *mockVariantRepoForStockExport) Update(_ context.Context, _ *catalog.Variant) error {
	return nil
}
func (m *mockVariantRepoForStockExport) WithTx(_ *sql.Tx) catalog.VariantRepository { return m }

// --- tests ---

func TestStockExport_Basic(t *testing.T) {
	now := time.Now()
	stockRepo := &mockStockRepo{
		entries: []inventory.StockEntry{
			{VariantID: "v1", Quantity: 10, UpdatedAt: now},
			{VariantID: "v2", Quantity: 25, UpdatedAt: now},
		},
	}
	varRepo := &mockVariantRepoForStockExport{
		variants: map[string]*catalog.Variant{
			"v1": {ID: "v1", SKU: "SKU-001"},
			"v2": {ID: "v2", SKU: "SKU-002"},
		},
	}

	exp := exporter.NewStockExporter(stockRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 2 {
		t.Errorf("Entries = %d, want 2", result.Entries)
	}

	records := parseCSV(t, &buf)
	if len(records) != 3 { // header + 2 data
		t.Fatalf("rows = %d, want 3", len(records))
	}
	if strings.Join(records[0], ",") != "sku,quantity" {
		t.Errorf("header = %v, want [sku quantity]", records[0])
	}
	if records[1][0] != "SKU-001" || records[1][1] != "10" {
		t.Errorf("row1 = %v, want [SKU-001 10]", records[1])
	}
	if records[2][0] != "SKU-002" || records[2][1] != "25" {
		t.Errorf("row2 = %v, want [SKU-002 25]", records[2])
	}
}

func TestStockExport_EmptyStock(t *testing.T) {
	stockRepo := &mockStockRepo{}
	varRepo := &mockVariantRepoForStockExport{variants: map[string]*catalog.Variant{}}

	exp := exporter.NewStockExporter(stockRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 0 {
		t.Errorf("Entries = %d, want 0", result.Entries)
	}
	records := parseCSV(t, &buf)
	if len(records) != 1 { // header only
		t.Errorf("rows = %d, want 1", len(records))
	}
}

func TestStockExport_OrphanStockSkipped(t *testing.T) {
	stockRepo := &mockStockRepo{
		entries: []inventory.StockEntry{
			{VariantID: "v1", Quantity: 10},
			{VariantID: "v-orphan", Quantity: 5},
		},
	}
	varRepo := &mockVariantRepoForStockExport{
		variants: map[string]*catalog.Variant{
			"v1": {ID: "v1", SKU: "SKU-001"},
		},
	}

	exp := exporter.NewStockExporter(stockRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 1 {
		t.Errorf("Entries = %d, want 1 (orphan skipped)", result.Entries)
	}
}

func TestStockExport_ListStockError(t *testing.T) {
	stockRepo := &mockStockRepo{listErr: fmt.Errorf("db unavailable")}
	varRepo := &mockVariantRepoForStockExport{variants: map[string]*catalog.Variant{}}

	exp := exporter.NewStockExporter(stockRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error for list stock failure")
	}
	if !strings.Contains(err.Error(), "db unavailable") {
		t.Errorf("error = %q, want containing 'db unavailable'", err.Error())
	}
}

func TestStockExport_Pagination(t *testing.T) {
	entries := make([]inventory.StockEntry, 150)
	variants := make(map[string]*catalog.Variant, 150)
	for i := range entries {
		vid := fmt.Sprintf("v%d", i+1)
		sku := fmt.Sprintf("SKU-%03d", i+1)
		entries[i] = inventory.StockEntry{VariantID: vid, Quantity: i + 1}
		variants[vid] = &catalog.Variant{ID: vid, SKU: sku}
	}
	stockRepo := &mockStockRepo{entries: entries}
	varRepo := &mockVariantRepoForStockExport{variants: variants}

	exp := exporter.NewStockExporter(stockRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Entries != 150 {
		t.Errorf("Entries = %d, want 150", result.Entries)
	}
	records := parseCSV(t, &buf)
	if len(records) != 151 {
		t.Fatalf("rows = %d, want 151", len(records))
	}
}
