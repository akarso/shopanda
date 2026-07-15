# Integration demo reference plugin

Reference integrator plugin for Phase 8 Track D (PR-833). Demonstrates:

- `RegisterSecureRoute` with API key + optional HMAC
- `Idempotency-Key` safe ERP retries (when API wires the Postgres idempotency store)
- Thin handler → `extapi.IntegrationOrderStatusUpdater` (no direct Postgres imports)

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

## Route

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

Use this plugin as a template for inbound ERP callbacks — copy patterns, do not import this package from production plugins.
