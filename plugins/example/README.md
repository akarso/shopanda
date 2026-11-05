# Example External Plugin

Reference implementation for third-party-style plugins living outside `plugins/core/`.

## What it demonstrates

- **Pricing step** — adds a fixed example fee (`example.fee`) to the pricing pipeline
- **Event sync job** — `RegisterOnEvent("demo.order_created", order.created, …)`. Checkout publishes `order.created` after the order is saved; if a later checkout step fails, compensation publishes `order.failed` (pending→failed). Serve wires Bus → queue with envelope `trigger=event`. Do **not** hand-roll `OnAsync` + `Enqueue` for this (that stamps `trigger=manual`). Use `Enqueue` only for true run-now. Handlers must tolerate a follow-up `order.failed`.
- **Admin permission** — registers `example.reports.read` for the Admin role
- **CLI command** — registers `example:ping` via `RegisterCommand`

## Enable

```yaml
plugins:
  example:
    enabled: true
    fee_minor_units: 100   # optional; default 100 (1.00 in major units when currency uses 2 decimals)
```

Or via environment:

```env
SHOPANDA_PLUGINS_EXAMPLE_ENABLED=true
SHOPANDA_PLUGINS_EXAMPLE_FEE_MINOR_UNITS=100
```

Restart the application after changing config. The plugin is registered in `cmd/api/register_plugins.go` when `plugins.example.enabled` is true.

When enabled, **Example fee (minor units)** can also be edited on **Admin → Integrations** without restart. Values persist to the config store and apply to the pricing step immediately.

Try the CLI hook:

```bash
app example:ping
```

List the sync job (requires `plugins.example.enabled=true`):

```bash
app plugins report
# … Sync jobs: integration.sync.example.demo.order_created  trigger=event(order.created)  example
```

## Authoring notes

- Implement `plugin.Plugin` with a unique `Name()` (use a vendor prefix, e.g. `acme/shipping`)
- Register extensions in `Init` via `plugin.App` / `pluginsdk` — do not modify core domain packages
- Keep infrastructure adapters in core plugins; external plugins should extend behavior through events, pipelines, permissions, and sync jobs
- Event → durable work: `RegisterOnEvent`. Run-now: `Enqueue` after boot. Keep any custom `OnAsync` body tiny; a failed bus enqueue is not retried by the event bus (durability starts once the queue accepts the job).
- After dequeue, event payloads are JSON maps (`payload["data"].(map[string]interface{})`), not domain structs — typed asserts only work on in-memory test queues.

See also: [DEVELOPER.md](../../docs/guides/DEVELOPER.md) · [PLUGINS.md](../../PLUGINS.md)
