# promodemo

Reference plugin for custom catalog promotion rule types via `PromotionRules` registration.

## Rule types

| Type | Kind | Config | Behavior |
| --- | --- | --- | --- |
| `min_line_total` | catalog condition | `value` (minor units) | Matches when line total ≥ value |
| `line_bonus_percent` | catalog action | `percentage` (1–100) | Discount = line total × percentage / 100 |

## Enable

```yaml
plugins:
  promodemo:
    enabled: true
```

Register during plugin `Init` before pricing steps are constructed (same pattern as export/mail ports).

## Example promotion JSON

```json
{
  "conditions": {"type": "min_line_total", "value": 5000},
  "actions": {"type": "line_bonus_percent", "percentage": 5}
}
```

Core JSON rule types (`always`, `percentage`, etc.) remain unchanged; unknown types delegate to the evaluator registry when registered.
