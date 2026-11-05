# Example External Plugin

Reference implementation for third-party-style plugins living outside `plugins/core/`.

## What it demonstrates

- **Pricing step** — adds a fixed example fee (`example.fee`) to the pricing pipeline
- **Manual sync job** — `extapi.Manual()` job `demo.order_created`; `OnAsync(order.created)` only enqueues (queue retry + jobs admin). Do not put slow I/O in the bus listener.
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

List the sync job:

```bash
app plugins report
# … Sync jobs: integration.sync.example.demo.order_created  trigger=manual  example
```

## Authoring notes

- Implement `plugin.Plugin` with a unique `Name()` (use a vendor prefix, e.g. `acme/shipping`)
- Register extensions in `Init` via `plugin.App` / `pluginsdk` — do not modify core domain packages
- Keep infrastructure adapters in core plugins; external plugins should extend behavior through events, pipelines, permissions, and sync jobs
- For durable/retryable work from an event: `RegisterManual` + `Enqueue` from a tiny `OnAsync` body (PR-1088). Inline third-party calls in `OnAsync` have no queue retry.

See also: [DEVELOPER.md](../../docs/guides/DEVELOPER.md) · [PLUGINS.md](../../PLUGINS.md)
