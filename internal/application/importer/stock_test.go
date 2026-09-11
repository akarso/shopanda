package importer_test

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/akarso/shopanda/internal/testutil"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/akarso/shopanda/internal/application/importer"
	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
)

// --- minimal ReindexService fakes: just enough to construct a real
// *searchApp.ReindexService and observe what it enqueues — see
// stock_sync_service_test.go's identical set for why these aren't shared
// across packages (small, package-local, not worth new shared test infra
// for). ---

type stockImportFakeRunStore struct{}

func (stockImportFakeRunStore) Create(context.Context, domainsearch.Run) error { return nil }
func (stockImportFakeRunStore) Get(context.Context, string) (*domainsearch.Run, error) {
	return nil, nil
}
func (stockImportFakeRunStore) UpdateProgress(context.Context, string, int, int, int) error {
	return nil
}
func (stockImportFakeRunStore) Finish(context.Context, string, domainsearch.RunStatus, string) error {
	return nil
}
func (stockImportFakeRunStore) FindStaleProcessing(context.Context, time.Time, int) ([]domainsearch.Run, error) {
	return nil, nil
}
func (stockImportFakeRunStore) List(context.Context, int, int) ([]domainsearch.Run, error) {
	return nil, nil
}

type stockImportFakeProductSource struct{ total int }

func (f stockImportFakeProductSource) CountAll(context.Context) (int, error) { return f.total, nil }
func (stockImportFakeProductSource) ListAll(context.Context, int, int) ([]domainsearch.Product, error) {
	return nil, nil
}
func (stockImportFakeProductSource) ListByIDs(context.Context, []string) ([]domainsearch.Product, error) {
	return nil, nil
}
func (stockImportFakeProductSource) ProductIDsByCategory(context.Context, []string) ([]string, error) {
	return nil, nil
}
func (stockImportFakeProductSource) ProductIDsUpdatedSince(context.Context, time.Time) ([]string, error) {
	return nil, nil
}

type stockImportFakeQueue struct {
	enqueued []domainjobs.Job
}

func (q *stockImportFakeQueue) Enqueue(_ context.Context, job domainjobs.Job) error {
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *stockImportFakeQueue) Dequeue(context.Context) (*domainjobs.Job, error) { return nil, nil }
func (q *stockImportFakeQueue) Complete(context.Context, string) error           { return nil }
func (q *stockImportFakeQueue) Fail(context.Context, string, error) error        { return nil }

func newTestReindexService(t *testing.T, queue *stockImportFakeQueue) *searchApp.ReindexService {
	t.Helper()
	svc, err := searchApp.NewReindexService(stockImportFakeRunStore{}, stockImportFakeProductSource{total: 1000}, queue, logger.New("error"), 0.2)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	return svc
}

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
func (m *mockVariantRepoForStock) FindBySKUs(_ context.Context, skus []string) (map[string]*catalog.Variant, error) {
	out := make(map[string]*catalog.Variant, len(skus))
	for _, sku := range skus {
		if v, ok := m.variants[sku]; ok {
			out[sku] = v
		}
	}
	return out, nil
}
func (m *mockVariantRepoForStock) ListByProductID(_ context.Context, _ string, _, _ int) ([]catalog.Variant, error) {
	return nil, nil
}
func (m *mockVariantRepoForStock) ListByProductIDs(ctx context.Context, productIDs []string, limitPerProduct int) (map[string][]catalog.Variant, error) {
	return testutil.ListByProductIDsFromList(ctx, m.ListByProductID, productIDs, limitPerProduct)
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

func (m *mockStockRepo) SetStocks(ctx context.Context, entries []inventory.StockEntry) error {
	for i := range entries {
		if err := m.SetStock(ctx, &entries[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockStockRepo) ListStock(_ context.Context, offset, limit int) ([]inventory.StockEntry, error) {
	return nil, nil
}

func (m *mockStockRepo) ListInventory(_ context.Context, _, _ int, _ string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}

func (m *mockStockRepo) GetInventoryItem(_ context.Context, variantID string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{VariantID: variantID}, nil
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

// TestStockImport_TriggersOneBatchedReindex pins PR-1049's fix: a CSV
// import touching multiple variants across multiple products must
// trigger exactly one ReindexService.Trigger call covering every touched
// product — not one per row, which would reproduce the fan-out PR-1036
// identified and avoided for bulk price import.
func TestStockImport_TriggersOneBatchedReindex(t *testing.T) {
	p1, p2 := id.New(), id.New()
	varRepo := &mockVariantRepoForStock{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001", ProductID: p1},
			"SKU-002": {ID: "v2", SKU: "SKU-002", ProductID: p1}, // same product as SKU-001
			"SKU-003": {ID: "v3", SKU: "SKU-003", ProductID: p2},
		},
	}
	stockRepo := newMockStockRepo()
	queue := &stockImportFakeQueue{}
	imp := importer.NewStockImporter(varRepo, stockRepo).WithReindex(newTestReindexService(t, queue))

	csv := "sku,quantity\nSKU-001,10\nSKU-002,20\nSKU-003,30\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 3 {
		t.Fatalf("Updated = %d, want 3", result.Updated)
	}

	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want exactly 1 (batched, not per-row)", len(queue.enqueued))
	}
	ids, ok := queue.enqueued[0].Payload["product_ids"].([]string)
	if !ok {
		t.Fatalf("payload product_ids type = %T, want []string", queue.enqueued[0].Payload["product_ids"])
	}
	sort.Strings(ids)
	want := []string{p1, p2}
	sort.Strings(want)
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Errorf("product_ids = %v, want %v (deduplicated across the 3 touched variants)", ids, want)
	}
}

// TestStockImport_NilReindexServiceIsNoop pins that Import works exactly
// as before when WithReindex was never used — the default for every
// existing caller/test that doesn't need reindex triggering.
func TestStockImport_NilReindexServiceIsNoop(t *testing.T) {
	varRepo := &mockVariantRepoForStock{
		variants: map[string]*catalog.Variant{
			"SKU-001": {ID: "v1", SKU: "SKU-001"},
		},
	}
	stockRepo := newMockStockRepo()
	imp := importer.NewStockImporter(varRepo, stockRepo)

	csv := "sku,quantity\nSKU-001,10\n"
	result, err := imp.Import(context.Background(), strings.NewReader(csv))
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", result.Updated)
	}
}
