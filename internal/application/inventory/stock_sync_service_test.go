package inventory

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	domainjobs "github.com/akarso/shopanda/internal/domain/jobs"
	domainsearch "github.com/akarso/shopanda/internal/domain/search"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/internal/platform/id"
	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
)

func init() {
	sql.Register("shopanda-stock-sync-fake-tx", stockSyncFakeTxDriver{})
}

type stockSyncFakeTxDriver struct{}

func (stockSyncFakeTxDriver) Open(string) (driver.Conn, error) { return stockSyncFakeTxConn{}, nil }

type stockSyncFakeTxConn struct{}

func (stockSyncFakeTxConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (stockSyncFakeTxConn) Close() error                        { return nil }
func (stockSyncFakeTxConn) Begin() (driver.Tx, error)           { return stockSyncFakeTx{}, nil }

type stockSyncFakeTx struct{}

func (stockSyncFakeTx) Commit() error   { return nil }
func (stockSyncFakeTx) Rollback() error { return nil }

var (
	stockSyncFakeTxOnce sync.Once
	stockSyncFakeTxDB   *sql.DB
)

func stockSyncFakeTxDBConn() *sql.DB {
	stockSyncFakeTxOnce.Do(func() {
		db, err := sql.Open("shopanda-stock-sync-fake-tx", "")
		if err != nil {
			panic(err)
		}
		stockSyncFakeTxDB = db
	})
	return stockSyncFakeTxDB
}

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
	err      error
}

func (q *stockSyncFakeQueue) Enqueue(_ context.Context, job domainjobs.Job) error {
	if q.err != nil {
		return q.err
	}
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

	// failOnSetStocksCall, when > 0, makes the Nth call to SetStocks
	// (1-indexed) return an error instead of writing — simulates a later
	// chunk failing after earlier chunks already committed.
	failOnSetStocksCall int
	setStocksCalls      int
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
	r.setStocksCalls++
	if r.failOnSetStocksCall > 0 && r.setStocksCalls == r.failOnSetStocksCall {
		return errors.New("stockSyncStockRepo: simulated batch failure")
	}
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
func (r *stockSyncStockRepo) WithTx(*sql.Tx) inventory.StockRepository { return r }

// stockSyncProductRepo is permissive unless byID is set. A set map returns
// only those rows (a missing id is a missing product).
type stockSyncProductRepo struct {
	byID map[string]*catalog.Product
	// lockOverride replaces the locked row for an id. FindByID still uses byID,
	// so a test can show a stale read losing to LockTypeAndModes.
	lockOverride map[string]*catalog.Product
}

func (r stockSyncProductRepo) FindByID(_ context.Context, id string) (*catalog.Product, error) {
	if r.byID != nil {
		return r.byID[id], nil
	}
	if id == "" {
		return nil, nil
	}
	return &catalog.Product{ID: id, Type: catalog.TypeSimple, Status: catalog.StatusActive, StockMode: catalog.StockModeFixed}, nil
}
func (stockSyncProductRepo) FindBySlug(context.Context, string) (*catalog.Product, error) {
	return nil, nil
}
func (stockSyncProductRepo) List(context.Context, catalog.ListFilter) ([]catalog.Product, error) {
	return nil, nil
}
func (stockSyncProductRepo) FindByCategoryID(context.Context, string, int, int) ([]catalog.Product, error) {
	return nil, nil
}
func (stockSyncProductRepo) Create(context.Context, *catalog.Product) error { return nil }
func (stockSyncProductRepo) Update(context.Context, *catalog.Product) error { return nil }
func (r stockSyncProductRepo) WithTx(*sql.Tx) catalog.ProductRepository     { return r }
func (r stockSyncProductRepo) LockTypeAndModes(_ context.Context, id string) (catalog.Type, catalog.PricingMode, catalog.StockMode, bool, catalog.Status, error) {
	if r.lockOverride != nil {
		if p, ok := r.lockOverride[id]; ok {
			if p == nil {
				return "", "", "", false, "", apperror.NotFound("product not found")
			}
			return p.Type, catalog.PricingModeFixed, p.StockMode, p.ExplodeOrderLines, p.Status, nil
		}
	}
	p, err := r.FindByID(context.Background(), id)
	if err != nil {
		return "", "", "", false, "", err
	}
	if p == nil {
		return "", "", "", false, "", apperror.NotFound("product not found")
	}
	mode := p.StockMode
	if mode == "" {
		mode = catalog.StockModeFixed
	}
	return p.Type, catalog.PricingModeFixed, mode, p.ExplodeOrderLines, p.Status, nil
}

func newTestStockSync(variants *stockSyncVariantRepo, stock *stockSyncStockRepo) *StockSyncService {
	return NewStockSyncService(variants, stock).WithProducts(stockSyncProductRepo{}).WithTxStarter(stockSyncFakeTxDBConn())
}

func TestStockSyncService_UpsertBySKU(t *testing.T) {
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1", ProductID: "p1"},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := newTestStockSync(variants, stock)

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
		"SKU-1": {ID: "var-1", SKU: "SKU-1", ProductID: "p1"},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := newTestStockSync(variants, stock)

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
	svc := newTestStockSync(variants, stock)
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
	svc := newTestStockSync(variants, stock)

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

// TestStockSyncService_UpsertBySKU_ReindexesProductsCommittedBeforeALaterChunkFails
// pins the code review fix: SetStocks writes in chunks of
// stockSyncChunkSize (100) rows, so a run with more than one chunk whose
// second chunk fails must still trigger a reindex covering whatever the
// first chunk already committed — the whole point of this PR is closing
// exactly this class of "written but never reindexed" gap, and the
// original implementation (reindex only on the success path) silently
// reintroduced it for any multi-chunk run with a late failure.
func TestStockSyncService_UpsertBySKU_ReindexesProductsCommittedBeforeALaterChunkFails(t *testing.T) {
	const total = stockSyncChunkSize + 50 // forces a second SetStocks chunk
	p1, p2 := id.New(), id.New()
	bySKU := make(map[string]*catalog.Variant, total)
	updates := make([]extapi.StockLevelUpdate, 0, total)
	for i := 0; i < total; i++ {
		sku := fmt.Sprintf("SKU-%03d", i)
		productID := p1
		if i >= stockSyncChunkSize {
			productID = p2
		}
		bySKU[sku] = &catalog.Variant{ID: fmt.Sprintf("v%03d", i), SKU: sku, ProductID: productID}
		updates = append(updates, extapi.StockLevelUpdate{SKU: sku, Quantity: 5})
	}
	variants := &stockSyncVariantRepo{bySKU: bySKU}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry), failOnSetStocksCall: 2}
	svc := newTestStockSync(variants, stock)
	queue := &stockSyncFakeQueue{}
	svc.SetReindexService(newTestReindexService(t, queue))

	result, err := svc.UpsertBySKU(context.Background(), updates)
	if err == nil {
		t.Fatal("expected an error from the second (simulated-failure) SetStocks chunk")
	}
	if result.Updated != stockSyncChunkSize {
		t.Fatalf("Updated = %d, want %d (only the successful first chunk)", result.Updated, stockSyncChunkSize)
	}

	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want exactly 1 (reindex must still fire despite the later chunk's failure)", len(queue.enqueued))
	}
	ids, ok := queue.enqueued[0].Payload["product_ids"].([]string)
	if !ok {
		t.Fatalf("payload product_ids type = %T, want []string", queue.enqueued[0].Payload["product_ids"])
	}
	if len(ids) != 1 || ids[0] != p1 {
		t.Errorf("product_ids = %v, want exactly [%s] — chunk 1 committed p1; p2's chunk failed and must not be reindexed", ids, p1)
	}
}

func TestStockSyncService_UpsertBySKU_TriggerErrorIsReturned(t *testing.T) {
	p1 := id.New()
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1", ProductID: p1},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := newTestStockSync(variants, stock)
	queueErr := errors.New("queue down")
	queue := &stockSyncFakeQueue{err: queueErr}
	svc.SetReindexService(newTestReindexService(t, queue))

	_, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-1", Quantity: 10},
	})
	if !errors.Is(err, queueErr) {
		t.Fatalf("err = %v, want queue down wrapped from Trigger", err)
	}
}

func TestStockSyncService_UpsertBySKU_RejectsDynamicBundle(t *testing.T) {
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-SIMPLE": {ID: "simple-v", SKU: "SKU-SIMPLE", ProductID: "simple"},
		"SKU-BUNDLE": {ID: "bundle-v", SKU: "SKU-BUNDLE", ProductID: "bundle"},
		"SKU-GONE":   {ID: "gone-v", SKU: "SKU-GONE", ProductID: "missing"},
	}}
	products := stockSyncProductRepo{byID: map[string]*catalog.Product{
		"simple": {ID: "simple", Type: catalog.TypeSimple, Status: catalog.StatusActive, StockMode: catalog.StockModeFixed},
		"bundle": {ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive, StockMode: catalog.StockModeDynamic},
	}}

	t.Run("dynamic bundle is skipped without an error and the simple sku is stored", func(t *testing.T) {
		stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
		svc := NewStockSyncService(variants, stock).WithProducts(products).WithTxStarter(stockSyncFakeTxDBConn())
		result, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
			{SKU: "SKU-SIMPLE", Quantity: 4},
			{SKU: "SKU-BUNDLE", Quantity: 9},
		})
		if err != nil {
			t.Fatalf("UpsertBySKU: %v, want nil so an ERP retry does not loop", err)
		}
		if _, wrote := stock.entries["bundle-v"]; wrote {
			t.Fatalf("dynamic bundle stock = %+v, want no row", stock.entries["bundle-v"])
		}
		if stock.entries["simple-v"].Quantity != 4 {
			t.Fatalf("simple stock = %+v, want quantity 4", stock.entries["simple-v"])
		}
		if result.Updated != 1 || result.Skipped != 1 {
			t.Fatalf("result = %+v, want Updated=1 Skipped=1", result)
		}
		if len(result.RejectedSKUs) != 1 || result.RejectedSKUs[0] != "SKU-BUNDLE" {
			t.Fatalf("RejectedSKUs = %v, want [SKU-BUNDLE]", result.RejectedSKUs)
		}
	})

	t.Run("missing product fails closed before any write", func(t *testing.T) {
		stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
		svc := NewStockSyncService(variants, stock).WithProducts(products)
		_, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
			{SKU: "SKU-SIMPLE", Quantity: 4},
			{SKU: "SKU-GONE", Quantity: 2},
		})
		if err == nil || !strings.Contains(err.Error(), "not found") {
			t.Fatalf("err = %v, want product not found", err)
		}
		if len(stock.entries) != 0 {
			t.Fatalf("stock = %+v, want no writes when a product lookup is missing", stock.entries)
		}
	})

	t.Run("nil product repository fails closed", func(t *testing.T) {
		stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
		svc := NewStockSyncService(variants, stock)
		_, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
			{SKU: "SKU-SIMPLE", Quantity: 4},
		})
		if err == nil || !strings.Contains(err.Error(), "product repository is required") {
			t.Fatalf("err = %v, want a missing repository error", err)
		}
		if len(stock.entries) != 0 {
			t.Fatalf("stock = %+v, want no writes", stock.entries)
		}
	})
}

func TestStockSyncService_UpsertBySKU_LockSeesDynamicMode(t *testing.T) {
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-SIMPLE": {ID: "simple-v", SKU: "SKU-SIMPLE", ProductID: "simple"},
		"SKU-BUNDLE": {ID: "bundle-v", SKU: "SKU-BUNDLE", ProductID: "bundle"},
	}}
	products := stockSyncProductRepo{
		byID: map[string]*catalog.Product{
			"simple": {ID: "simple", Type: catalog.TypeSimple, Status: catalog.StatusActive, StockMode: catalog.StockModeFixed},
			"bundle": {ID: "bundle", Type: catalog.TypeBundle, Status: catalog.StatusActive, StockMode: catalog.StockModeFixed},
		},
		lockOverride: map[string]*catalog.Product{
			"bundle": {ID: "bundle", Type: catalog.TypeBundle, StockMode: catalog.StockModeDynamic},
		},
	}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := NewStockSyncService(variants, stock).WithProducts(products).WithTxStarter(stockSyncFakeTxDBConn())

	result, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-SIMPLE", Quantity: 4},
		{SKU: "SKU-BUNDLE", Quantity: 9},
	})
	if err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if _, wrote := stock.entries["bundle-v"]; wrote {
		t.Fatalf("dynamic bundle stock = %+v, want no row", stock.entries["bundle-v"])
	}
	if stock.entries["simple-v"].Quantity != 4 {
		t.Fatalf("simple stock = %+v, want quantity 4", stock.entries["simple-v"])
	}
	if result.Updated != 1 || result.Skipped != 1 {
		t.Fatalf("result = %+v, want Updated=1 Skipped=1", result)
	}
	if len(result.RejectedSKUs) != 1 || result.RejectedSKUs[0] != "SKU-BUNDLE" {
		t.Fatalf("RejectedSKUs = %v, want [SKU-BUNDLE]", result.RejectedSKUs)
	}
}

type stockSyncFakeBundleParents struct {
	byComponent map[string][]string
}

func (f *stockSyncFakeBundleParents) ListBundleIDsByComponentID(_ context.Context, componentProductID string) ([]string, error) {
	if f.byComponent == nil {
		return []string{}, nil
	}
	return append([]string(nil), f.byComponent[componentProductID]...), nil
}

// TestStockSyncService_UpsertBySKU_ReindexExpandsBundleParents pins PR-1080
// bulk ERP stock Trigger expansion through BundleParentLookup.
func TestStockSyncService_UpsertBySKU_ReindexExpandsBundleParents(t *testing.T) {
	leafID, parentID := id.New(), id.New()
	variants := &stockSyncVariantRepo{bySKU: map[string]*catalog.Variant{
		"SKU-1": {ID: "var-1", SKU: "SKU-1", ProductID: leafID},
	}}
	stock := &stockSyncStockRepo{entries: make(map[string]inventory.StockEntry)}
	svc := newTestStockSync(variants, stock)
	queue := &stockSyncFakeQueue{}
	svc.SetReindexService(newTestReindexService(t, queue))
	svc.SetBundleParents(&stockSyncFakeBundleParents{byComponent: map[string][]string{
		leafID: {parentID},
	}})

	if _, err := svc.UpsertBySKU(context.Background(), []extapi.StockLevelUpdate{
		{SKU: "SKU-1", Quantity: 10},
	}); err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.enqueued))
	}
	ids, ok := queue.enqueued[0].Payload["product_ids"].([]string)
	if !ok {
		t.Fatalf("payload product_ids type = %T", queue.enqueued[0].Payload["product_ids"])
	}
	sort.Strings(ids)
	want := []string{leafID, parentID}
	sort.Strings(want)
	if len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("product_ids = %v, want %v", ids, want)
	}
}
