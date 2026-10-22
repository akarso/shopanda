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

// stockSyncProducts is the product read UpsertBySKU uses to reject dynamic
// bundles. ListByIDs is optional; postgres implements it.
type stockSyncProducts interface {
	FindByID(ctx context.Context, id string) (*catalog.Product, error)
}

type stockSyncProductLister interface {
	ListByIDs(ctx context.Context, ids []string) (map[string]*catalog.Product, error)
}

// StockSyncService upserts warehouse stock levels by variant SKU.
type StockSyncService struct {
	variants catalog.VariantRepository
	products stockSyncProducts
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

// WithProducts wires the product read that rejects stock writes onto a
// dynamic bundle. UpsertBySKU fails closed when this is unset and a known
// SKU needs that lookup, and when the product row is missing.
func (s *StockSyncService) WithProducts(products stockSyncProducts) *StockSyncService {
	s.products = products
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

	type stockSyncCandidate struct {
		sku     string
		variant *catalog.Variant
		qty     int
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

	entries := make([]inventory.StockEntry, 0, len(candidates))
	entryProductIDs := make([]string, 0, len(candidates))
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

		entry, entryErr := inventory.NewStockEntry(candidate.variant.ID, candidate.qty)
		if entryErr != nil {
			return result, apperror.Validation(entryErr.Error())
		}
		entries = append(entries, entry)
		entryProductIDs = append(entryProductIDs, candidate.variant.ProductID)
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
