package inventory

import (
	"context"
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
const reindexTriggerTimeout = 30 * time.Second

// StockSyncService upserts warehouse stock levels by variant SKU.
type StockSyncService struct {
	variants catalog.VariantRepository
	stock    inventory.StockRepository

	// reindex is optional — see SetReindexService.
	reindex *searchApp.ReindexService
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

// UpsertBySKU sets absolute stock quantities for known variant SKUs.
// Unknown SKUs are skipped and listed in the result; invalid rows increment Skipped.
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

	entries := make([]inventory.StockEntry, 0, len(validBySKU))
	entryProductIDs := make([]string, 0, len(validBySKU))
	for _, sku := range uniqueSKUs {
		variant := variantsBySKU[sku]
		if variant == nil {
			result.Skipped++
			result.UnknownSKUs = append(result.UnknownSKUs, sku)
			continue
		}

		entry, entryErr := inventory.NewStockEntry(variant.ID, validBySKU[sku])
		if entryErr != nil {
			return result, apperror.Validation(entryErr.Error())
		}
		entries = append(entries, entry)
		entryProductIDs = append(entryProductIDs, variant.ProductID)
	}

	for i := 0; i < len(entries); i += stockSyncChunkSize {
		end := i + stockSyncChunkSize
		if end > len(entries) {
			end = len(entries)
		}
		if setErr := s.stock.SetStocks(ctx, entries[i:end]); setErr != nil {
			return result, fmt.Errorf("stock sync: set stock batch: %w", setErr)
		}
		for _, productID := range entryProductIDs[i:end] {
			touchedProductIDs[productID] = struct{}{}
		}
		result.Updated += end - i
	}

	return result, nil
}

// triggerReindex fires a single scoped reindex covering every product this
// sync run actually committed — see SetReindexService's own doc comment
// for why this is one batched call, not one per row. A Trigger failure is
// returned to the caller (via UpsertBySKU's named result) so a queue,
// scope-resolution, or database error is actionable rather than leaving
// search silently stale.
func (s *StockSyncService) triggerReindex(touchedProductIDs map[string]struct{}) error {
	if s.reindex == nil || len(touchedProductIDs) == 0 {
		return nil
	}
	ids := make([]string, 0, len(touchedProductIDs))
	for id := range touchedProductIDs {
		ids = append(ids, id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), reindexTriggerTimeout)
	defer cancel()
	if _, err := s.reindex.Trigger(ctx, searchApp.ScopeProducts{IDs: ids}); err != nil {
		return fmt.Errorf("stock sync: reindex: %w", err)
	}
	return nil
}
