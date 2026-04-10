package exporter_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/akarso/shopanda/internal/application/exporter"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
)

// --- price test mocks ---

type mockPriceRepoForExport struct {
	prices  []pricing.Price
	listErr error
}

func (m *mockPriceRepoForExport) FindByVariantAndCurrency(_ context.Context, _, _ string) (*pricing.Price, error) {
	return nil, nil
}

func (m *mockPriceRepoForExport) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (m *mockPriceRepoForExport) List(_ context.Context, offset, limit int) ([]pricing.Price, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if offset >= len(m.prices) {
		return nil, nil
	}
	end := offset + limit
	if end > len(m.prices) {
		end = len(m.prices)
	}
	return m.prices[offset:end], nil
}

func (m *mockPriceRepoForExport) Upsert(_ context.Context, _ *pricing.Price) error {
	return nil
}

type mockVariantRepoForPriceExport struct {
	variants map[string]*catalog.Variant // keyed by ID
}

func (m *mockVariantRepoForPriceExport) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	return m.variants[id], nil
}
func (m *mockVariantRepoForPriceExport) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForPriceExport) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForPriceExport) Create(_ context.Context, _ *catalog.Variant) error {
	return nil
}
func (m *mockVariantRepoForPriceExport) Update(_ context.Context, _ *catalog.Variant) error {
	return nil
}
func (m *mockVariantRepoForPriceExport) WithTx(_ *sql.Tx) catalog.VariantRepository { return m }

func makePrice(id, variantID string, amount int64, currency string) pricing.Price {
	money := shared.MustNewMoney(amount, currency)
	p, _ := pricing.NewPrice(id, variantID, money)
	return p
}

// --- tests ---

func TestPriceExport_Basic(t *testing.T) {
	priceRepo := &mockPriceRepoForExport{
		prices: []pricing.Price{
			makePrice("p1", "v1", 1999, "EUR"),
			makePrice("p2", "v1", 2199, "USD"),
			makePrice("p3", "v2", 999, "EUR"),
		},
	}
	varRepo := &mockVariantRepoForPriceExport{
		variants: map[string]*catalog.Variant{
			"v1": {ID: "v1", SKU: "SKU-001"},
			"v2": {ID: "v2", SKU: "SKU-002"},
		},
	}

	exp := exporter.NewPriceExporter(priceRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 3 {
		t.Errorf("Entries = %d, want 3", result.Entries)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4 (header + 3 data)", len(lines))
	}
	if lines[0] != "sku,currency,amount" {
		t.Errorf("header = %q, want sku,currency,amount", lines[0])
	}
	if lines[1] != "SKU-001,EUR,1999" {
		t.Errorf("line 1 = %q, want SKU-001,EUR,1999", lines[1])
	}
}

func TestPriceExport_Empty(t *testing.T) {
	priceRepo := &mockPriceRepoForExport{}
	varRepo := &mockVariantRepoForPriceExport{variants: map[string]*catalog.Variant{}}
	exp := exporter.NewPriceExporter(priceRepo, varRepo)

	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 0 {
		t.Errorf("Entries = %d, want 0", result.Entries)
	}
	if strings.TrimSpace(buf.String()) != "sku,currency,amount" {
		t.Errorf("output = %q, want header only", buf.String())
	}
}

func TestPriceExport_ListError(t *testing.T) {
	priceRepo := &mockPriceRepoForExport{listErr: fmt.Errorf("db error")}
	varRepo := &mockVariantRepoForPriceExport{variants: map[string]*catalog.Variant{}}
	exp := exporter.NewPriceExporter(priceRepo, varRepo)

	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err == nil {
		t.Fatal("expected error on list failure")
	}
}

func TestPriceExport_OrphanPrice(t *testing.T) {
	// Price for a variant that no longer exists should be skipped.
	priceRepo := &mockPriceRepoForExport{
		prices: []pricing.Price{
			makePrice("p1", "v1", 1999, "EUR"),
			makePrice("p2", "v-gone", 999, "EUR"),
		},
	}
	varRepo := &mockVariantRepoForPriceExport{
		variants: map[string]*catalog.Variant{
			"v1": {ID: "v1", SKU: "SKU-001"},
		},
	}

	exp := exporter.NewPriceExporter(priceRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 1 {
		t.Errorf("Entries = %d, want 1 (orphan skipped)", result.Entries)
	}
}

func TestPriceExport_FormulaInjection(t *testing.T) {
	priceRepo := &mockPriceRepoForExport{
		prices: []pricing.Price{
			makePrice("p1", "v1", 1999, "EUR"),
		},
	}
	varRepo := &mockVariantRepoForPriceExport{
		variants: map[string]*catalog.Variant{
			"v1": {ID: "v1", SKU: "=cmd|' /C calc'!A0"},
		},
	}

	exp := exporter.NewPriceExporter(priceRepo, varRepo)
	var buf bytes.Buffer
	_, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines")
	}
	// SKU should be sanitized with leading quote.
	if !strings.HasPrefix(lines[1], "'") {
		t.Errorf("line 1 = %q, want sanitized SKU starting with '", lines[1])
	}
}

func TestPriceExport_VariantCaching(t *testing.T) {
	// Same variant appears in multiple prices — FindByID must be called only once per unique variant.
	priceRepo := &mockPriceRepoForExport{
		prices: []pricing.Price{
			makePrice("p1", "v1", 1999, "EUR"),
			makePrice("p2", "v1", 2199, "USD"),
			makePrice("p3", "v1", 2499, "GBP"),
		},
	}
	varRepo := &countingVariantRepo{
		variants: map[string]*catalog.Variant{
			"v1": {ID: "v1", SKU: "SKU-001"},
		},
	}

	exp := exporter.NewPriceExporter(priceRepo, varRepo)
	var buf bytes.Buffer
	result, err := exp.Export(context.Background(), &buf)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if result.Entries != 3 {
		t.Errorf("Entries = %d, want 3", result.Entries)
	}
	if varRepo.findByIDCalls != 1 {
		t.Errorf("FindByID calls = %d, want 1 (should be cached)", varRepo.findByIDCalls)
	}
}

// countingVariantRepo wraps variant lookups and counts FindByID calls.
type countingVariantRepo struct {
	variants      map[string]*catalog.Variant
	findByIDCalls int
}

func (m *countingVariantRepo) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	m.findByIDCalls++
	return m.variants[id], nil
}
func (m *countingVariantRepo) FindBySKU(_ context.Context, _ string) (*catalog.Variant, error) {
	return nil, nil
}
func (m *countingVariantRepo) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *countingVariantRepo) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *countingVariantRepo) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *countingVariantRepo) WithTx(_ *sql.Tx) catalog.VariantRepository         { return m }
