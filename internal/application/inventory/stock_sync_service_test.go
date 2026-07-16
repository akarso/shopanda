package inventory

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/internal/domain/catalog"
	"github.com/akarso/shopanda/internal/domain/inventory"
	"github.com/akarso/shopanda/pkg/extapi"
)

type stockSyncVariantRepo struct {
	bySKU map[string]*catalog.Variant
}

func (r *stockSyncVariantRepo) FindBySKU(_ context.Context, sku string) (*catalog.Variant, error) {
	if r.bySKU == nil {
		return nil, nil
	}
	return r.bySKU[sku], nil
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
	})
	if err != nil {
		t.Fatalf("UpsertBySKU: %v", err)
	}
	if result.Updated != 1 || result.Skipped != 2 || len(result.UnknownSKUs) != 1 || result.UnknownSKUs[0] != "MISSING" {
		t.Fatalf("result = %+v", result)
	}
	if stock.entries["var-1"].Quantity != 10 {
		t.Fatalf("stock = %+v", stock.entries["var-1"])
	}
}
