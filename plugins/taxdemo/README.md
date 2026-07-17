# Tax demo reference plugin

Reference integrator plugin for Phase 8 Track F (PR-853). Demonstrates:

- `RegisterTaxCalculator` — replace the core tax calculator port
- `RegisterPricingStep(..., replace:tax)` — occupy the core tax pipeline slot (PR-852)

## Enable

```yaml
plugins:
  taxdemo:
    enabled: true
    flat_rate_bps: 1900
```

Or environment:

```bash
SHOPANDA_PLUGINS_TAXDEMO_ENABLED=true
SHOPANDA_PLUGINS_TAXDEMO_FLAT_RATE_BPS=1900
```

## Behavior

When enabled, the plugin registers a flat-rate `tax.Calculator` and replaces the core `tax` pricing step with `taxdemo.flat_tax`. Tax still uses the standard `PricingContext.Meta` keys (`tax_country`, `tax_mode`); the configured basis-points rate applies to every line instead of the rate table.

Use this plugin as a template for external VAT/ERP tax services — copy patterns, do not import this package from production plugins.
