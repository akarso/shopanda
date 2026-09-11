package inventory

import (
	"context"
	"fmt"
	"strings"

	searchApp "github.com/akarso/shopanda/internal/application/search"
	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/internal/platform/apperror"
	"github.com/akarso/shopanda/pkg/extapi"
)

const stockSyncChunkSize = 100

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
func (s *StockSyncService) UpsertBySKU(ctx context.Context, updates []extapi.StockLevelUpdate) (extapi.StockSyncResult, error) {
	result := extapi.StockSyncResult{}
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

	variantsBySKU := make(map[string]*catalog.Variant, len(uniqueSKUs))
	for i := 0; i < len(uniqueSKUs); i += stockSyncChunkSize {
		end := i + stockSyncChunkSize
		if end > len(uniqueSKUs) {
			end = len(uniqueSKUs)
		}
		found, err := s.variants.FindBySKUs(ctx, uniqueSKUs[i:end])
		if err != nil {
			return result, fmt.Errorf("stock sync: find variants by sku: %w", err)
		}
		for sku, variant := range found {
			variantsBySKU[sku] = variant
		}
	}

	entries := make([]inventory.StockEntry, 0, len(validBySKU))
	touchedProductIDs := make(map[string]struct{}, len(validBySKU))
	for _, sku := range uniqueSKUs {
		variant := variantsBySKU[sku]
		if variant == nil {
			result.Skipped++
			result.UnknownSKUs = append(result.UnknownSKUs, sku)
			continue
		}

		entry, err := inventory.NewStockEntry(variant.ID, validBySKU[sku])
		if err != nil {
			return result, apperror.Validation(err.Error())
		}
		entries = append(entries, entry)
		touchedProductIDs[variant.ProductID] = struct{}{}
		result.Updated++
	}

	for i := 0; i < len(entries); i += stockSyncChunkSize {
		end := i + stockSyncChunkSize
		if end > len(entries) {
			end = len(entries)
		}
		if err := s.stock.SetStocks(ctx, entries[i:end]); err != nil {
			return result, fmt.Errorf("stock sync: set stock batch: %w", err)
		}
	}

	s.triggerReindex(ctx, touchedProductIDs)
	return result, nil
}

// triggerReindex fires a single scoped reindex covering every product this
// sync run touched — see SetReindexService's own doc comment for why this
// is one batched call, not one per row. Best-effort: the stock sync
// itself already committed successfully by this point, so a reindex
// failure is not worth turning a successful sync into a reported error
// over (the existing manual/scheduled search:reindex remains the
// fallback, same as any other on-save indexing gap in this codebase).
func (s *StockSyncService) triggerReindex(ctx context.Context, touchedProductIDs map[string]struct{}) {
	if s.reindex == nil || len(touchedProductIDs) == 0 {
		return
	}
	ids := make([]string, 0, len(touchedProductIDs))
	for id := range touchedProductIDs {
		ids = append(ids, id)
	}
	_, _ = s.reindex.Trigger(ctx, searchApp.ScopeProducts{IDs: ids})
}
