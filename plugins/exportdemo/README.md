# exportdemo — reference export remap plugin

Demonstrates ERP column remap on `export:products` via `export.product.row` hook.

## Enable

```yaml
plugins:
  exportdemo:
    enabled: true
```

## Behavior

| Core column | ERP column |
| --- | --- |
| `sku` | `matnr` |
| `name` | `maktx` |
| `description` | `maktx2` |
| `slug` | `ext_slug` |

Core columns are removed from the row after remap when a value was copied.

## Example

```bash
app export:products erp-products.csv
```

Output header includes `matnr`, `maktx`, … instead of core column names.
