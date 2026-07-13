# Cart demo reference plugin

Reference integrator plugin for Phase 8 Track B (PR-814). Demonstrates:

- `cart.validate` — per-line minimum quantity with structured storefront errors
- `RegisterPricingStep(..., "after:promotions")` — flat handling fee

## Enable

```yaml
plugins:
  cartdemo:
    enabled: true
    min_quantity: 2
    handling_fee_minor_units: 50
```

Or environment:

```bash
SHOPANDA_PLUGINS_CARTDEMO_ENABLED=true
SHOPANDA_PLUGINS_CARTDEMO_MIN_QUANTITY=2
SHOPANDA_PLUGINS_CARTDEMO_HANDLING_FEE_MINOR_UNITS=50
```

## Behavior

When enabled, adding a cart line with quantity below `min_quantity` returns HTTP 422 with `validation_errors` containing code `cartdemo.min_quantity`. Valid carts receive a flat handling fee during pricing recalculate.

Use this plugin as a template for custom cart rules — copy patterns, do not import this package from production plugins.
