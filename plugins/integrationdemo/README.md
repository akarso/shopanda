# Integration demo reference plugin

Reference integrator plugin for Phase 8 Track D (PR-833) and PR-1400. Demonstrates:

- `RegisterSecureRoute` with API key + optional HMAC
- `Idempotency-Key` safe ERP retries (when API wires the Postgres idempotency store)
- Thin handlers → `extapi.IntegrationOrderStatusUpdater` / `IntegrationShipmentStatusUpdater` (no direct Postgres imports)

## Enable

```yaml
plugins:
  integrationdemo:
    enabled: true
    integration_api_key: "change-me"
    integration_hmac_secret: "" # optional
```

Or environment:

```bash
SHOPANDA_PLUGINS_INTEGRATIONDEMO_ENABLED=true
SHOPANDA_PLUGINS_INTEGRATIONDEMO_INTEGRATION_API_KEY=change-me
```

## Routes

### Order status

```http
POST /api/v1/integrations/integrationdemo/order-status
X-Integration-Key: change-me
Idempotency-Key: erp-callback-001
Content-Type: application/json
```

### Flat JSON body

```json
{
  "order_id": "ord_abc123",
  "status": "CONFIRMED",
  "external_ref": "90001234"
}
```

### Simplified SAP IDoc wrapper

```json
{
  "E1ORDSTAT": {
    "order_id": "ord_abc123",
    "status": "CONFIRMED",
    "VBELN": "90001234"
  }
}
```

Supported status values: domain statuses (`confirmed`, `paid`, …) or ERP codes (`CONFIRMED`, `PAID`, `CANCELLED`, `FAILED`).

## Response

```json
{
  "order_status": {
    "order_id": "ord_abc123",
    "status": "confirmed",
    "previous_status": "pending",
    "changed": true,
    "external_ref": "90001234"
  }
}
```

Repeating the same request with the same `Idempotency-Key` replays the first response (`X-Idempotency-Replayed: true`).

### Shipment status (PR-1400)

```http
POST /api/v1/integrations/integrationdemo/shipment-status
X-Integration-Key: change-me
Idempotency-Key: wh-callback-001
Content-Type: application/json
```

```json
{
  "order_id": "ord_abc123",
  "status": "SHIPPED",
  "tracking_number": "1Z999",
  "provider_ref": "carrier-job-42",
  "external_ref": "WH-90001"
}
```

Supported statuses: `shipped` (tracking required), `delivered`, `cancelled` (or `SHIPPED` / `DELIVERED` / `CANCELLED`). Response key: `shipment_status`.

`external_ref` is echoed on the response only — it is not persisted. Concurrent update races return **409** `shipment.conflict` (rotate `Idempotency-Key` after 5xx; 409 is safe to retry with a new key after re-reading state). Missing tracking / blank `order_id` on ship is **400** `shipment.invalid_payload`. Already-shipped retries with the same tracking are idempotent even if `provider_ref` is omitted (and republish `shipment.shipped` for email heal); a different tracking (or conflicting non-empty `provider_ref`), or ship/deliver on a cancelled/failed order, is **422** `shipment.invalid_transition`. Cancel is irreversible for the order (unique shipment row; no reopen API).

Use this plugin as a template for inbound ERP/warehouse callbacks — copy patterns, do not import this package from production plugins.
