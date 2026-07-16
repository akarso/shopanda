package extapi

import (
	"context"
	"errors"
)

// Integration stock sync error codes for structured logging and plugin handling.
const (
	IntegrationErrorStockUnknownSKU    = "stock.unknown_sku"
	IntegrationErrorStockInvalidQuantity = "stock.invalid_quantity"
)

var (
	// ErrIntegrationStockUnknownSKU indicates a warehouse SKU is not in catalog.
	ErrIntegrationStockUnknownSKU = errors.New("integration stock unknown sku")
)

// StockLevelUpdate is one absolute stock quantity keyed by variant SKU.
type StockLevelUpdate struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// StockSyncResult summarizes a warehouse stock pull upsert run.
type StockSyncResult struct {
	Updated     int      `json:"updated"`
	Skipped     int      `json:"skipped"`
	UnknownSKUs []string `json:"unknown_skus,omitempty"`
}

// IntegrationStockSyncer upserts absolute stock levels by SKU for outbound warehouse sync.
type IntegrationStockSyncer interface {
	UpsertBySKU(ctx context.Context, updates []StockLevelUpdate) (StockSyncResult, error)
}
