# Example External Plugin

Reference implementation for third-party-style plugins living outside `plugins/core/`.

## What it demonstrates

- **Pricing step** — adds a fixed example fee (`example.fee`) to the pricing pipeline
- **Event listener** — async handler on `order.created` that logs order metadata
- **Admin permission** — registers `example.reports.read` for the Admin role

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

## Authoring notes

- Implement `plugin.Plugin` with a unique `Name()` (use a vendor prefix, e.g. `acme/shipping`)
- Register extensions in `Init` via `plugin.App` — do not modify core domain packages
- Keep infrastructure adapters in core plugins; external plugins should extend behavior through events, pipelines, and permissions

See also: [DEVELOPER.md](../../docs/guides/DEVELOPER.md) · [PLUGINS.md](../../PLUGINS.md)
