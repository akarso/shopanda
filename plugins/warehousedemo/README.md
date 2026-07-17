# warehousedemo — outbound warehouse stock reference plugin

Demonstrates **Track E** outbound integration: pull absolute stock levels from a mock warehouse REST API on a cron schedule and upsert via `extapi.IntegrationStockSyncer` (no direct Postgres imports).

## Flow

1. Scheduler enqueues `integration.sync.warehousedemo.warehouse.stock` on cron (default `@every 5m`).
2. Worker executes the registered handler.
3. Handler `GET {warehouse_base_url}/stock` using `pkg/integrationsdk/http`.
4. Response JSON upserted through `IntegrationStockSyncer.UpsertBySKU`.

Expected warehouse response:

```json
{
  "stock": [
    { "sku": "SKU-1", "quantity": 42 }
  ]
}
```

Unknown SKUs are skipped and logged in the sync result; invalid rows (empty SKU, negative quantity) are skipped.

## Enable

```yaml
plugins:
  warehousedemo:
    enabled: true
    warehouse_base_url: "http://localhost:9090"
    warehouse_api_key: optional-bearer-token
    sync_cron: "@every 5m"
```

Environment variables:

- `SHOPANDA_PLUGINS_WAREHOUSEDEMO_ENABLED`
- `SHOPANDA_PLUGINS_WAREHOUSEDEMO_WAREHOUSE_BASE_URL`
- `SHOPANDA_PLUGINS_WAREHOUSEDEMO_WAREHOUSE_API_KEY`
- `SHOPANDA_PLUGINS_WAREHOUSEDEMO_SYNC_CRON`

## Boundaries

| Layer | Responsibility |
| --- | --- |
| Plugin | HTTP pull + sync job registration |
| `extapi.IntegrationStockSyncer` | Upsert port |
| `internal/application/inventory.StockSyncService` | SKU lookup + stock persistence |

See also: `plugins/integrationdemo` (inbound Track D), `docs/guides/PLUGIN_COMPOSITION.md` (outbound section).
