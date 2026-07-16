package warehousedemo

import (
	"context"
	"fmt"
	"net/http"

	"github.com/akarso/shopanda/internal/platform/logger"
	"github.com/akarso/shopanda/pkg/extapi"
	sdkhttp "github.com/akarso/shopanda/pkg/integrationsdk/http"
)

// NewStockSyncHandler returns a sync job handler that pulls stock and upserts via the port.
func NewStockSyncHandler(client *sdkhttp.Client, syncer extapi.IntegrationStockSyncer, log logger.Logger) extapi.SyncJobHandler {
	return func(ctx context.Context, job extapi.SyncJobContext) error {
		if client == nil {
			return fmt.Errorf("warehousedemo: http client not configured")
		}
		if syncer == nil {
			return fmt.Errorf("warehousedemo: stock syncer not configured")
		}

		var resp StockResponse
		if err := client.DoJSON(ctx, http.MethodGet, StockPath, nil, nil, &resp); err != nil {
			return fmt.Errorf("warehousedemo: pull stock: %w", err)
		}

		updates := make([]extapi.StockLevelUpdate, 0, len(resp.Stock))
		for _, row := range resp.Stock {
			updates = append(updates, extapi.StockLevelUpdate{
				SKU:      row.SKU,
				Quantity: row.Quantity,
			})
		}

		result, err := syncer.UpsertBySKU(ctx, updates)
		if err != nil {
			return fmt.Errorf("warehousedemo: upsert stock: %w", err)
		}

		fields := map[string]interface{}{
			"job_id":       job.JobID,
			"updated":      result.Updated,
			"skipped":      result.Skipped,
			"unknown_skus": len(result.UnknownSKUs),
		}
		if job.Logger != nil {
			job.Logger.Info("warehousedemo stock sync complete", fields)
		} else if log != nil {
			log.Info("warehousedemo stock sync complete", fields)
		}
		return nil
	}
}
