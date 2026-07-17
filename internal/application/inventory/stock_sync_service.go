package inventory

import (
	"context"
	"fmt"
	"strings"

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
	return result, nil
}
