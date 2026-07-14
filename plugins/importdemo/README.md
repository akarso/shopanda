# Import demo reference plugin

Reference integrator plugin for Phase 8 Track C (PR-823). Demonstrates:

- `import.product.row` — remap SAP-style ERP CSV columns to core product import columns

## Enable

```yaml
plugins:
  importdemo:
    enabled: true
```

Or environment:

```bash
SHOPANDA_PLUGINS_IMPORTDEMO_ENABLED=true
```

## ERP column mapping

| ERP column | Core column |
| --- | --- |
| `matnr` | `sku` |
| `maktx` | `name` |
| `maktx2` | `description` |
| `ext_slug` | `slug` |

When `slug` is still empty after remap, it is derived from `sku` (lowercased, non-alphanumeric → `-`).

Missing `matnr`/`sku` after remap records structured error `importdemo.missing_sku`.

## Example CSV

```csv
matnr,maktx,maktx2
SKU-001,Widget,A fine widget
```

Import:

```bash
app import:products erp-products.csv
```

Use this plugin as a template for ERP column normalization — copy patterns, do not import this package from production plugins.
