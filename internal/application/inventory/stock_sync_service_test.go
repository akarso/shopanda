package inventory

import (
	"context"
	"sort"
	"testing"
	"time"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
)

// --- minimal ReindexService fakes: just enough to construct a real
// *searchApp.ReindexService and observe what it enqueues, for the
// "exactly one batched Trigger call" tests below. ---

type stockSyncFakeRunStore struct{}

func (stockSyncFakeRunStore) Create(context.Context, domainsearch.Run) error { return nil }
func (stockSyncFakeRunStore) Get(context.Context, string) (*domainsearch.Run, error) {
	return nil, nil
}
func (stockSyncFakeRunStore) UpdateProgress(context.Context, string, int, int, int) error { return nil }
func (stockSyncFakeRunStore) Finish(context.Context, string, domainsearch.RunStatus, string) error {
	return nil
}
func (stockSyncFakeRunStore) FindStaleProcessing(context.Context, time.Time, int) ([]domainsearch.Run, error) {
	return nil, nil
}
func (stockSyncFakeRunStore) List(context.Context, int, int) ([]domainsearch.Run, error) {
	return nil, nil
}

type stockSyncFakeProductSource struct {
	total int
}

func (f stockSyncFakeProductSource) CountAll(context.Context) (int, error) { return f.total, nil }
func (stockSyncFakeProductSource) ListAll(context.Context, int, int) ([]domainsearch.Product, error) {
	return nil, nil
}
func (stockSyncFakeProductSource) ListByIDs(context.Context, []string) ([]domainsearch.Product, error) {
	return nil, nil
}
func (stockSyncFakeProductSource) ProductIDsByCategory(context.Context, []string) ([]string, error) {
	return nil, nil
}
func (stockSyncFakeProductSource) ProductIDsUpdatedSince(context.Context, time.Time) ([]string, error) {
	return nil, nil
}

// stockSyncFakeQueue records every Enqueue call so tests can assert
// exactly how many jobs a run produced, and inspect each one's payload.
type stockSyncFakeQueue struct {
	enqueued []domainjobs.Job
}

func (q *stockSyncFakeQueue) Enqueue(_ context.Context, job domainjobs.Job) error {
	q.enqueued = append(q.enqueued, job)
	return nil
}
func (q *stockSyncFakeQueue) Dequeue(context.Context) (*domainjobs.Job, error) { return nil, nil }
func (q *stockSyncFakeQueue) Complete(context.Context, string) error           { return nil }
func (q *stockSyncFakeQueue) Fail(context.Context, string, error) error        { return nil }

func newTestReindexService(t *testing.T, queue *stockSyncFakeQueue) *searchApp.ReindexService {
	t.Helper()
	svc, err := searchApp.NewReindexService(stockSyncFakeRunStore{}, stockSyncFakeProductSource{total: 1000}, queue, logger.New("error"), 0.2)
	if err != nil {
		t.Fatalf("NewReindexService: %v", err)
	}
	return svc
}

type stockSyncVariantRepo struct {
	bySKU map[string]*catalog.Variant
}

func (r *stockSyncVariantRepo) FindBySKU(_ context.Context, sku string) (*catalog.Variant, error) {
	if r.bySKU == nil {
		return nil, nil
	}
	return r.bySKU[sku], nil
}

func (r *stockSyncVariantRepo) FindBySKUs(_ context.Context, skus []string) (map[string]*catalog.Variant, error) {
	out := make(map[string]*catalog.Variant, len(skus))
	for _, sku := range skus {
		if v := r.bySKU[sku]; v != nil {
			out[sku] = v
		}
	}
	return out, nil
}

func (r *stockSyncVariantRepo) FindByID(context.Context, string) (*catalog.Variant, error) {
	return nil, nil
}
func (r *stockSyncVariantRepo) ListByProductID(context.Context, string, int, int) ([]catalog.Variant, error) {
	return nil, nil
}
func (r *stockSyncVariantRepo) ListByProductIDs(context.Context, []string, int) (map[string][]catalog.Variant, error) {
	return nil, nil
}
func (r *stockSyncVariantRepo) Create(context.Context, *catalog.Variant) error { return nil }
func (r *stockSyncVariantRepo) Update(context.Context, *catalog.Variant) error { return nil }

type stockSyncStockRepo struct {
	entries map[string]inventory.StockEntry
}

func (r *stockSyncStockRepo) GetStock(_ context.Context, variantID string) (inventory.StockEntry, error) {
	if entry, ok := r.entries[variantID]; ok {
		return entry, nil
	}
	return inventory.StockEntry{VariantID: variantID}, nil
}
func (r *stockSyncStockRepo) SetStock(_ context.Context, entry *inventory.StockEntry) error {
	if r.entries == nil {
		r.entries = make(map[string]inventory.StockEntry)
	}
	r.entries[entry.VariantID] = *entry
	return nil
}
func (r *stockSyncStockRepo) SetStocks(_ context.Context, entries []inventory.StockEntry) error {
	for i := range entries {
		if err := r.SetStock(context.Background(), &entries[i]); err != nil {
			return err
		}
	}
	return nil
}
func (r *stockSyncStockRepo) ListStock(context.Context, int, int) ([]inventory.StockEntry, error) {
	return nil, nil
}
func (r *stockSyncStockRepo) ListInventory(context.Context, int, int, string) ([]inventory.InventoryListItem, error) {
	return nil, nil
}
func (r *stockSyncStockRepo) GetInventoryItem(context.Context, string) (inventory.InventoryListItem, error) {
	return inventory.InventoryListItem{}, nil
}

func TestStockSyncService_UpsertBySKU(t *testing.T) {
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1"},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := NewStockSyncService(variants, stock)

	result, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-1", Quantity: 10},
		{SKU: "MISSING", Quantity: 3},
		{SKU: "", Quantity: 1},
		{SKU: "SKU-1", Quantity: -5},
	})
	if err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if result.Updated != 1 || result.Skipped != 3 || len(result.UnknownSKUs) != 1 || result.UnknownSKUs[0] != "MISSING" {
		t.Fatalf("result = %+v", result)
	}
	if stock.entries["var-1"].Quantity != 10 {
		t.Fatalf("stock = %+v", stock.entries["var-1"])
	}
}

func TestStockSyncService_UpsertBySKU_DeduplicatesLatestQuantity(t *testing.T) {
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1"},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := NewStockSyncService(variants, stock)

	result, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-1", Quantity: 10},
		{SKU: "SKU-1", Quantity: 25},
	})
	if err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if result.Updated != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	if stock.entries["var-1"].Quantity != 25 {
		t.Fatalf("stock = %+v", stock.entries["var-1"])
	}
}

// TestStockSyncService_UpsertBySKU_TriggersOneBatchedReindex pins PR-1049's
// fix: a sync run touching multiple variants across multiple products
// must trigger exactly one ReindexService.Trigger call covering every
// touched product — not one per row, which would reproduce the fan-out
// PR-1036 identified and avoided for bulk price import.
func TestStockSyncService_UpsertBySKU_TriggersOneBatchedReindex(t *testing.T) {
	p1, p2 := id.New(), id.New()
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1", ProductID: p1},
		"SKU-2": {ID: "var-2", SKU: "SKU-2", ProductID: p1}, // same product as SKU-1
		"SKU-3": {ID: "var-3", SKU: "SKU-3", ProductID: p2},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := NewStockSyncService(variants, stock)
	queue := &stockSyncFakeQueue{}
	svc.SetReindexService(newTestReindexService(t, queue))

	result, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-1", Quantity: 10},
		{SKU: "SKU-2", Quantity: 20},
		{SKU: "SKU-3", Quantity: 30},
	})
	if err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if result.Updated != 3 {
		t.Fatalf("result = %+v, want Updated=3", result)
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

// TestStockSyncService_UpsertBySKU_NilReindexServiceIsNoop pins that
// UpsertBySKU works exactly as before when SetReindexService was never
// called — the default for every existing caller/test that doesn't need
// reindex triggering, matching InventoryAdminHandler.SetBus's own
// optional-wiring convention.
func TestStockSyncService_UpsertBySKU_NilReindexServiceIsNoop(t *testing.T) {
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1", ProductID: "p1"},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := NewStockSyncService(variants, stock)

	result, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-1", Quantity: 10},
	})
	if err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("result = %+v, want Updated=1", result)
	}
}
