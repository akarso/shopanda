package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/pkg/extapi"
)

const stockSyncChunkSize = 100

// reindexTriggerTimeout bounds the post-commit ReindexService.Trigger call
// so it does not inherit a canceled operation context (worker shutdown or
// a deadline that already expired after earlier chunks committed).
// Expansion uses its own timeout of the same length so a large parent
// lookup cannot expire the enqueue context.
const reindexTriggerTimeout = 30 * time.Second

// stockReindexExpandMaxIDs is the largest touched set we expand to parent
// bundles. At or above this, ReindexService.Trigger already substitutes a
// full scan for a scoped ID list (maxScopedReindexProductIDs), so
// expansion is wasted work and would compete with the enqueue timeout.
const stockReindexExpandMaxIDs = 10_000

// stockSyncProducts is the product read UpsertBySKU uses to reject dynamic
// bundles. ListByIDs is optional; postgres implements it.
type stockSyncProducts interface {
	FindByID(ctx context.Context, id string) (*catalog.Product, error)
}

type stockSyncProductLister interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error)
}

// stockSyncTxStarter begins the transaction that locks a product row and
// writes its stock. *sql.DB implements it.
type stockSyncTxStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

type stockSyncTxProducts interface {
	WithTx(tx *sql.Tx) catalog.ProductRepository
}

type stockSyncLockingProducts interface {
	catalog.ProductRepository
	LockTypeAndModes(ctx context.Context, id string) (catalog.Type, catalog.PricingMode, catalog.StockMode, bool, catalog.Status, error)
}

type stockSyncTxStock interface {
	inventory.StockRepository
	WithTx(tx *sql.Tx) inventory.StockRepository
}

type stockSyncCandidate struct {
	sku     string
	variant *catalog.Variant
	qty     int
}

// StockSyncService upserts warehouse stock levels by variant SKU.
type StockSyncService struct {
	variants  catalog.VariantRepository
	products  stockSyncProducts
	stock     inventory.StockRepository
	txStarter stockSyncTxStarter

	// reindex is optional — see SetReindexService.
	reindex *searchApp.ReindexService
	// bundleParents is optional — see SetBundleParents.
	bundleParents catalog.BundleParentLookup
}

// NewStockSyncService returns a StockSyncService backed by variants and stock repos.
func NewStockSyncService(variants catalog.VariantRepository, stock inventory.StockRepository) *StockSyncService {
	if variants == nil {
		panic("inventory: stock sync service variants repository must not be nil")
	}
	if stock == nil {
		panic("inventory: stock sync service stock repository must not be nil")
	}
	return &StockSyncService{variants: variants, stock: stock}
}

// WithProducts wires the product read that rejects stock writes onto a
// dynamic bundle. UpsertBySKU fails closed when this is unset and a known
// SKU needs that lookup, and when the product row is missing.
func (s *StockSyncService) WithProducts(products stockSyncProducts) *StockSyncService {
	s.products = products
	return s
}

// WithTxStarter wires the transaction beginner for the product-row lock and
// the stock write. UpsertBySKU fails closed when this is unset and a row is
// ready to write. *sql.DB satisfies stockSyncTxStarter.
func (s *StockSyncService) WithTxStarter(starter stockSyncTxStarter) *StockSyncService {
	s.txStarter = starter
	return s
}

// SetReindexService enables triggering one scoped reindex per UpsertBySKU
// call, covering every product it actually touched (PR-1049: ERP stock
// sync is a bulk stock-mutation path, so this publishes no per-row
// inventory.EventStockUpdated at all — that would reproduce the exact
// one-job-per-row fan-out risk PR-1036 identified and deliberately
// avoided for bulk price import. A single batched
// ReindexService.Trigger(ScopeProducts{...}) call after the whole sync
// completes covers the same ground, and ReindexService's own full-scan-
// threshold/hard-cap logic (PR-1034) already takes over automatically if
// the touched set turns out to be large.). Left unset (the default),
// UpsertBySKU works exactly as before — no reindex triggered, no error —
// matching InventoryAdminHandler.SetBus's own optional-wiring convention.
func (s *StockSyncService) SetReindexService(reindex *searchApp.ReindexService) {
	s.reindex = reindex
}

// SetBundleParents expands the post-sync reindex set with parent bundles
// that include each touched leaf (PR-1080). Bulk ERP sync publishes no
// per-row EventStockUpdated, so IndexUpdateSubscriber cannot fan out —
// expand here before Trigger. Nil clears the lookup.
func (s *StockSyncService) SetBundleParents(lookup catalog.BundleParentLookup) {
	s.bundleParents = lookup
}

// UpsertBySKU sets absolute stock quantities for known variant SKUs.
// Unknown SKUs are skipped and listed in the result; invalid rows increment Skipped.
// A dynamic-bundle SKU is not written. It increments Skipped and is listed in
// RejectedSKUs. The call returns nil after the remaining rows are stored so
// an ERP retry does not loop on a validation error for a row that was
// intentionally skipped. A missing product for a known SKU fails the call
// before any write.
//
// Updated and touchedProductIDs are recorded only after each SetStocks
// chunk succeeds, so a later chunk failing cannot inflate the result or
// reindex products whose write never committed. Reindex is triggered via
// defer so an early-return error path still covers whatever did commit;
// Trigger runs on a bounded independent context and its error is returned
// when the sync itself otherwise succeeded.
func (s *StockSyncService) UpsertBySKU(ctx context.Context, updates []extapi.StockLevelUpdate) (result extapi.StockSyncResult, err error) {
	touchedProductIDs := make(map[string]struct{}, len(updates))
	defer func() {
		if terr := s.triggerReindex(touchedProductIDs); terr != nil && err == nil {
			err = terr
		}
	}()

	validBySKU := make(map[string]int, len(updates))

	for _, update := range updates {
		sku := strings.TrimSpace(update.SKU)
		if sku == "" {
			result.Skipped++
			continue
		}
		if update.Quantity < 0 {
			result.Skipped++
			continue
		}
		validBySKU[sku] = update.Quantity
	}

	uniqueSKUs := make([]string, 0, len(validBySKU))
	for sku := range validBySKU {
		uniqueSKUs = append(uniqueSKUs, sku)
	}
	sort.Strings(uniqueSKUs)

	variantsBySKU := make(map[string]*catalog.Variant, len(uniqueSKUs))
	for i := 0; i < len(uniqueSKUs); i += stockSyncChunkSize {
		end := i + stockSyncChunkSize
		if end > len(uniqueSKUs) {
			end = len(uniqueSKUs)
		}
		found, findErr := s.variants.FindBySKUs(ctx, uniqueSKUs[i:end])
		if findErr != nil {
			return result, fmt.Errorf("stock sync: find variants by sku: %w", findErr)
		}
		for sku, variant := range found {
			variantsBySKU[sku] = variant
		}
	}

	candidates := make([]stockSyncCandidate, 0, len(validBySKU))
	for _, sku := range uniqueSKUs {
		variant := variantsBySKU[sku]
		if variant == nil {
			result.Skipped++
			result.UnknownSKUs = append(result.UnknownSKUs, sku)
			continue
		}
		if strings.TrimSpace(variant.ProductID) == "" {
			return result, fmt.Errorf("stock sync: sku %q has no product", sku)
		}
		candidates = append(candidates, stockSyncCandidate{sku: sku, variant: variant, qty: validBySKU[sku]})
	}

	productIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		productIDs = append(productIDs, candidate.variant.ProductID)
	}
	productsByID, err := s.productsByID(ctx, productIDs)
	if err != nil {
		return result, err
	}

	writable := make([]stockSyncCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		product := productsByID[candidate.variant.ProductID]
		if product == nil {
			return result, fmt.Errorf("stock sync: product %s for sku %q not found", candidate.variant.ProductID, candidate.sku)
		}
		if product.Type == catalog.TypeBundle && product.StockMode == catalog.StockModeDynamic {
			result.Skipped++
			result.RejectedSKUs = append(result.RejectedSKUs, candidate.sku)
			continue
		}
		if _, entryErr := inventory.NewStockEntry(candidate.variant.ID, candidate.qty); entryErr != nil {
			return result, apperror.Validation(entryErr.Error())
		}
		writable = append(writable, candidate)
	}

	for i := 0; i < len(writable); i += stockSyncChunkSize {
		end := i + stockSyncChunkSize
		if end > len(writable) {
			end = len(writable)
		}
		updated, rejected, touched, writeErr := s.writeStockChunk(ctx, writable[i:end])
		if writeErr != nil {
			return result, writeErr
		}
		result.Updated += updated
		result.Skipped += len(rejected)
		result.RejectedSKUs = append(result.RejectedSKUs, rejected...)
		for _, productID := range touched {
			touchedProductIDs[productID] = struct{}{}
		}
	}

	return result, nil
}

// writeStockChunk locks each product row and writes the chunk's stock on
// that same transaction. A bundle that is dynamic under the lock is skipped.
// Lock order is product id, then variant id, then the stock write.
func (s *StockSyncService) writeStockChunk(ctx context.Context, chunk []stockSyncCandidate) (updated int, rejected []string, touched []string, err error) {
	if len(chunk) == 0 {
		return 0, nil, nil, nil
	}
	if s.txStarter == nil {
		return 0, nil, nil, fmt.Errorf("stock sync: transaction starter is required")
	}
	if s.products == nil {
		return 0, nil, nil, fmt.Errorf("stock sync: product repository is required")
	}
	txProducts, ok := s.products.(stockSyncTxProducts)
	if !ok {
		return 0, nil, nil, fmt.Errorf("stock sync: product repository does not support transactions")
	}
	txStock, ok := s.stock.(stockSyncTxStock)
	if !ok {
		return 0, nil, nil, fmt.Errorf("stock sync: stock repository does not support transactions")
	}

	tx, err := s.txStarter.BeginTx(ctx, nil)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("stock sync: begin tx: %w", err)
	}
	defer tx.Rollback()

	locker, ok := txProducts.WithTx(tx).(stockSyncLockingProducts)
	if !ok {
		return 0, nil, nil, fmt.Errorf("stock sync: product repository does not support row locks")
	}
	boundStock := txStock.WithTx(tx)

	ordered := append([]stockSyncCandidate(nil), chunk...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].variant.ProductID != ordered[j].variant.ProductID {
			return ordered[i].variant.ProductID < ordered[j].variant.ProductID
		}
		return ordered[i].variant.ID < ordered[j].variant.ID
	})

	dynamic := make(map[string]bool, len(ordered))
	for _, candidate := range ordered {
		productID := candidate.variant.ProductID
		if _, seen := dynamic[productID]; seen {
			continue
		}
		productType, _, stockMode, _, _, lockErr := locker.LockTypeAndModes(ctx, productID)
		if lockErr != nil {
			if apperror.Is(lockErr, apperror.CodeNotFound) {
				return 0, nil, nil, fmt.Errorf("stock sync: product %s for sku %q not found", productID, candidate.sku)
			}
			return 0, nil, nil, fmt.Errorf("stock sync: lock product %s: %w", productID, lockErr)
		}
		dynamic[productID] = productType == catalog.TypeBundle && stockMode == catalog.StockModeDynamic
	}

	entries := make([]inventory.StockEntry, 0, len(ordered))
	for _, candidate := range ordered {
		if dynamic[candidate.variant.ProductID] {
			rejected = append(rejected, candidate.sku)
			continue
		}
		entry, entryErr := inventory.NewStockEntry(candidate.variant.ID, candidate.qty)
		if entryErr != nil {
			return 0, nil, nil, apperror.Validation(entryErr.Error())
		}
		entries = append(entries, entry)
		touched = append(touched, candidate.variant.ProductID)
	}
	if len(entries) > 0 {
		if setErr := boundStock.SetStocks(ctx, entries); setErr != nil {
			return 0, nil, nil, fmt.Errorf("stock sync: set stock batch: %w", setErr)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, nil, nil, fmt.Errorf("stock sync: commit: %w", err)
	}
	return len(entries), rejected, touched, nil
}

// productsByID loads every product a known SKU points at. A nil repository
// fails closed once a lookup is required. Missing rows are omitted; the
// caller rejects those SKUs.
func (s *StockSyncService) productsByID(ctx context.Context, ids []string) (map[string]*catalog.Product, error) {
	if len(ids) == 0 {
		return map[string]*catalog.Product{}, nil
	}
	if s.products == nil {
		return nil, fmt.Errorf("stock sync: product repository is required")
	}
	loadCtx := catalog.WithOperatorProductReadScope(ctx)
	if lister, ok := s.products.(stockSyncProductLister); ok {
		byID, err := lister.ListByIDs(loadCtx, ids)
		if err != nil {
			return nil, fmt.Errorf("stock sync: list products: %w", err)
		}
		if byID == nil {
			return map[string]*catalog.Product{}, nil
		}
		return byID, nil
	}
	byID := make(map[string]*catalog.Product, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		product, err := s.products.FindByID(loadCtx, id)
		if err != nil {
			return nil, fmt.Errorf("stock sync: find product %s: %w", id, err)
		}
		if product != nil {
			byID[id] = product
		}
	}
	return byID, nil
}

// triggerReindex fires a single scoped reindex covering every product this
// sync run actually committed — see SetReindexService's own doc comment
// for why this is one batched call, not one per row. When SetBundleParents
// is wired, parent kit IDs are merged in (PR-1080). Expansion and Trigger
// each get their own timeout so a large parent lookup cannot starve
// enqueue. A set at or above stockReindexExpandMaxIDs skips expansion
// and Triggers a full scan. A Trigger failure is returned to the caller
// (via UpsertBySKU's named result) so a queue, scope-resolution, or
// database error is actionable rather than leaving search silently stale.
func (s *StockSyncService) triggerReindex(touchedProductIDs map[string]struct{}) error {
	if s.reindex == nil || len(touchedProductIDs) == 0 {
		return nil
	}
	if len(touchedProductIDs) >= stockReindexExpandMaxIDs {
		ctx, cancel := context.WithTimeout(context.Background(), reindexTriggerTimeout)
		defer cancel()
		if _, err := s.reindex.Trigger(ctx, searchApp.ScopeAll{}); err != nil {
			return fmt.Errorf("stock sync: reindex: %w", err)
		}
		return nil
	}

	expandCtx, expandCancel := context.WithTimeout(context.Background(), reindexTriggerTimeout)
	expandErr := catalog.ExpandTouchedWithBundleParents(expandCtx, s.bundleParents, touchedProductIDs)
	expandCancel()

	ids := make([]string, 0, len(touchedProductIDs))
	for id := range touchedProductIDs {
		ids = append(ids, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reindexTriggerTimeout)
	defer cancel()
	if _, err := s.reindex.Trigger(ctx, searchApp.ScopeProducts{IDs: ids}); err != nil {
		return fmt.Errorf("stock sync: reindex: %w", err)
	}
	if expandErr != nil {
		return fmt.Errorf("stock sync: expand bundle parents: %w", expandErr)
	}
	return nil
}
