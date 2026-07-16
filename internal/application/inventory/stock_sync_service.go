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

		variant, err := s.variants.FindBySKU(ctx, sku)
		if err != nil {
			return result, fmt.Errorf("stock sync: find variant by sku %q: %w", sku, err)
		}
		if variant == nil {
			result.Skipped++
			result.UnknownSKUs = append(result.UnknownSKUs, sku)
			continue
		}

		entry, err := inventory.NewStockEntry(variant.ID, update.Quantity)
		if err != nil {
			return result, apperror.Validation(err.Error())
		}
		if err := s.stock.SetStock(ctx, &entry); err != nil {
			return result, fmt.Errorf("stock sync: set stock for sku %q: %w", sku, err)
		}
		result.Updated++
	}
	return result, nil
}
