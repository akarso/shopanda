package importer_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/importer"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/pricing"
	"github.com/akarso/shopanda/internal/domain/shared"
	"github.com/akarso/shopanda/internal/platform/event"
)

// --- price test mocks ---

type mockVariantRepoForPrice struct {
	variants map[string]*catalog.Variant // keyed by SKU
}

func (m *mockVariantRepoForPrice) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	for _, v := range m.variants {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, nil
}
func (m *mockVariantRepoForPrice) FindBySKU(_ context.Context, sku string) (*catalog.Variant, error) {
	return m.variants[sku], nil
}
func (m *mockVariantRepoForPrice) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForPrice) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepoForPrice) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *mockVariantRepoForPrice) WithTx(_ *sql.Tx) catalog.VariantRepository         { return m }

type mockPriceRepoForImport struct {
	prices    map[string]*pricing.Price // "variantID:currency" → price
	upsertErr error
	findErr   error
}

func newMockPriceRepoForImport() *mockPriceRepoForImport {
	return &mockPriceRepoForImport{prices: make(map[string]*pricing.Price)}
}

func (m *mockPriceRepoForImport) FindByVariantCurrencyAndStore(_ context.Context, variantID, currency, storeID string) (*pricing.Price, error) {
	if m.findErr != nil {
		return nil, m.findErr
	}
	return m.prices[variantID+":"+currency+":"+storeID], nil
}

func (m *mockPriceRepoForImport) ListByVariantID(_ context.Context, _ string) ([]pricing.Price, error) {
	return nil, nil
}

func (m *mockPriceRepoForImport) List(_ context.Context, _, _ int) ([]pricing.Price, error) {
	return nil, nil
}

func (m *mockPriceRepoForImport) Upsert(_ context.Context, p *pricing.Price) error {
	if m.upsertErr != nil {
		return m.upsertErr
	}
	key := p.VariantID + ":" + p.Amount.Currency() + ":" + p.StoreID
	m.prices[key] = p
	return nil
}

func priceVariants() *mockVariantRepoForPrice {
	return &mockVariantRepoForPrice{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
			"SKU-002": {ID: "v2", SKU: "SKU-002"},
		},
	}
}

// --- tests ---

func TestPriceImport_Basic(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,1999\nSKU-001,USD,2199\nSKU-002,EUR,999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 3 {
		t.Errorf("Created = %d, want 3", result.Created)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}

	// Verify prices stored.
	p := prices.prices["v1:EUR:"]
	if p == nil {
		t.Fatal("v1:EUR: not found")
	}
	if p.Amount.Amount() != 1999 {
		t.Errorf("v1:EUR amount = %d, want 1999", p.Amount.Amount())
	}
	if p.Amount.Currency() != "EUR" {
		t.Errorf("v1:EUR currency = %q, want EUR", p.Amount.Currency())
	}
}

func TestPriceImport_MissingColumns(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,amount\nSKU-001,1999\n"
	_, err := imp.Import(context.Background(), strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error for missing currency column")
	}
}

func TestPriceImport_EmptySKU(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\n,EUR,1999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if len(result.Errors) != 1 {
		t.Fatalf("Errors count = %d, want 1", len(result.Errors))
	}
	if !strings.Contains(result.Errors[0], "empty sku") {
		t.Errorf("error = %q, want mention of empty sku", result.Errors[0])
	}
}

func TestPriceImport_InvalidCurrency(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,euro,1999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "invalid currency") {
		t.Errorf("error = %q, want mention of invalid currency", result.Errors[0])
	}
}

func TestPriceImport_InvalidAmount(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,abc\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "invalid amount") {
		t.Errorf("error = %q, want mention of invalid amount", result.Errors[0])
	}
}

func TestPriceImport_NegativeAmount(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,-100\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "must be positive") {
		t.Errorf("error = %q, want mention of positive", result.Errors[0])
	}
}

func TestPriceImport_ZeroAmount(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,0\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
}

func TestPriceImport_UnknownSKU(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nNONEXIST,EUR,1999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if !strings.Contains(result.Errors[0], "unknown sku") {
		t.Errorf("error = %q, want mention of unknown sku", result.Errors[0])
	}
}

func TestPriceImport_Update(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	// Seed existing price.
	existing, _ := pricing.NewPrice("existing-id", "v1", "", shared.MustNewMoney(1000, "EUR"))
	prices.prices["v1:EUR:"] = &existing

	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,2500\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Updated != 1 {
		t.Errorf("Updated = %d, want 1", result.Updated)
	}
	if result.Created != 0 {
		t.Errorf("Created = %d, want 0", result.Created)
	}

	p := prices.prices["v1:EUR:"]
	if p.Amount.Amount() != 2500 {
		t.Errorf("amount = %d, want 2500", p.Amount.Amount())
	}
}

func TestPriceImport_UpsertError(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	prices.upsertErr = fmt.Errorf("db down")
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,1999\n"
	_, err := imp.Import(context.Background(), strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error on upsert failure")
	}
}

func TestPriceImport_CurrencyNormalization(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	// lowercase currency should be uppercased.
	input := "sku,currency,amount\nSKU-001,eur,1999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	p := prices.prices["v1:EUR:"]
	if p == nil {
		t.Fatal("v1:EUR: not found after lowercase currency input")
	}
}

func TestPriceImport_BOMHeader(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	// UTF-8 BOM (\xEF\xBB\xBF) before first column header, as exported by
	// Excel and some spreadsheet applications.
	input := "\xEF\xBB\xBFsku,currency,amount\nSKU-001,EUR,1999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 1 {
		t.Errorf("Created = %d, want 1", result.Created)
	}
	if result.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", result.Skipped)
	}
}

func TestPriceImport_SKUCaching(t *testing.T) {
	// Same SKU with two currencies — FindBySKU should be called only once.
	variants := &countingVariantRepoForPrice{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
		},
	}
	prices := newMockPriceRepoForImport()
	imp := importer.NewPriceImporter(variants, prices, nil, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,1999\nSKU-001,USD,2199\nSKU-001,GBP,2499\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 3 {
		t.Errorf("Created = %d, want 3", result.Created)
	}
	if variants.findBySKUCalls != 1 {
		t.Errorf("FindBySKU calls = %d, want 1 (should be cached)", variants.findBySKUCalls)
	}
}

type countingVariantRepoForPrice struct {
	variants       map[string]*catalog.Variant
	findBySKUCalls int
}

func (m *countingVariantRepoForPrice) FindByID(_ context.Context, id string) (*catalog.Variant, error) {
	for _, v := range m.variants {
		if v.ID == id {
			return v, nil
		}
	}
	return nil, nil
}
func (m *countingVariantRepoForPrice) FindBySKU(_ context.Context, sku string) (*catalog.Variant, error) {
	m.findBySKUCalls++
	return m.variants[sku], nil
}
func (m *countingVariantRepoForPrice) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *countingVariantRepoForPrice) Create(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *countingVariantRepoForPrice) Update(_ context.Context, _ *catalog.Variant) error { return nil }
func (m *countingVariantRepoForPrice) WithTx(_ *sql.Tx) catalog.VariantRepository         { return m }

// --- history mock ---

type mockPriceHistoryRepoForImport struct {
	recorded []*pricing.PriceSnapshot
	err      error
}

func (m *mockPriceHistoryRepoForImport) Record(_ context.Context, s *pricing.PriceSnapshot) error {
	if m.err != nil {
		return m.err
	}
	m.recorded = append(m.recorded, s)
	return nil
}

func (m *mockPriceHistoryRepoForImport) LowestSince(_ context.Context, _, _, _ string, _ time.Time) (*pricing.PriceSnapshot, error) {
	return nil, nil
}

// --- history tests ---

func TestPriceImport_RecordsHistory(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	history := &mockPriceHistoryRepoForImport{}
	imp := importer.NewPriceImporter(variants, prices, history, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,1999\nSKU-002,EUR,999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 2 {
		t.Errorf("Created = %d, want 2", result.Created)
	}
	if len(history.recorded) != 2 {
		t.Fatalf("recorded snapshots = %d, want 2", len(history.recorded))
	}
	if history.recorded[0].VariantID != "v1" {
		t.Errorf("snapshot[0].VariantID = %q, want v1", history.recorded[0].VariantID)
	}
	if history.recorded[1].VariantID != "v2" {
		t.Errorf("snapshot[1].VariantID = %q, want v2", history.recorded[1].VariantID)
	}
}

func TestPriceImport_RecordHistoryError(t *testing.T) {
	variants := priceVariants()
	prices := newMockPriceRepoForImport()
	history := &mockPriceHistoryRepoForImport{err: fmt.Errorf("history db down")}
	imp := importer.NewPriceImporter(variants, prices, history, nil, nil)

	input := "sku,currency,amount\nSKU-001,EUR,1999\n"
	_, err := imp.Import(context.Background(), strings.NewReader(input))
	if err == nil {
		t.Fatal("expected error when Record fails")
	}
	if !strings.Contains(err.Error(), "history db down") {
		t.Errorf("error = %q, want mention of 'history db down'", err)
	}
}

func TestPriceImport_PublishesEvent(t *testing.T) {
	variants := &mockVariantRepoForPrice{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", ProductID: "p1", SKU: "SKU-001"},
		},
	}
	prices := newMockPriceRepoForImport()
	log := &noopLogger{}
	bus := event.NewBus(log)

	var received []pricing.PriceUpsertedData
	bus.On(pricing.EventPriceUpserted, func(_ context.Context, evt event.Event) error {
		d := evt.Data.(pricing.PriceUpsertedData)
		received = append(received, d)
		return nil
	})

	imp := importer.NewPriceImporter(variants, prices, nil, nil, bus)

	input := "sku,currency,amount\nSKU-001,EUR,1999\n"
	result, err := imp.Import(context.Background(), strings.NewReader(input))
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if result.Created != 1 {
		t.Fatalf("Created = %d, want 1", result.Created)
	}
	if len(received) != 1 {
		t.Fatalf("events received = %d, want 1", len(received))
	}
	if received[0].ProductID != "p1" {
		t.Errorf("event ProductID = %q, want p1", received[0].ProductID)
	}
	if received[0].Currency != "EUR" {
		t.Errorf("event Currency = %q, want EUR", received[0].Currency)
	}
	if received[0].Amount != 1999 {
		t.Errorf("event Amount = %d, want 1999", received[0].Amount)
	}
}

type noopLogger struct{}

func (l *noopLogger) Info(_ string, _ map[string]interface{})           {}
func (l *noopLogger) Warn(_ string, _ map[string]interface{})           {}
func (l *noopLogger) Error(_ string, _ error, _ map[string]interface{}) {}
