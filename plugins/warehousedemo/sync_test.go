package warehousedemo

import (
	"context"
	"testing"

	"github.com/akarso/shopanda/pkg/extapi"
)

type noopStockSyncer struct{}

func (noopStockSyncer) UpsertBySKU(context.Context, []extapi.StockLevelUpdate) (extapi.StockSyncResult, error) {
	return extapi.StockSyncResult{}, nil
}

func TestNewStockSyncHandler_NilClient(t *testing.T) {
	handler := NewStockSyncHandler(nil, noopStockSyncer{}, nil)
	if err := handler(context.Background(), extapi.SyncJobContext{}); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestNewStockSyncHandler_NilSyncer(t *testing.T) {
	handler := NewStockSyncHandler(nil, nil, nil)
	if err := handler(context.Background(), extapi.SyncJobContext{}); err == nil {
		t.Fatal("expected error for nil syncer")
	}
}
