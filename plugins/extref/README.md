# extref — public SDK reference plugin

Track E (**PR-1551**) reference that uses **only** [`pkg/pluginsdk`](../../pkg/pluginsdk) and [`pkg/extapi`](../../pkg/extapi). Register with `pluginsdk.Adapt(extref.New())` in `cmd/api/register_plugins.go` (gated by `plugins.extref.enabled`).

## What it demonstrates

| Pattern | Where |
| --- | --- |
| `pluginsdk.Plugin` + `Needs()` (`CapCart`) | `plugin.go` |
| `RegisterField` — plugin-only `cart_item` field | `extref.gift_note` |
| `HookCartAddItemBefore` — read `extensions` + `actor_role` | `hooks.go` |
| `Host.WithPluginWrite` + `CartPort.AddItem` | `cart_write.go` |
| `RegisterPublicRoute` stub | `GET /extref/health` |

## Enable

```yaml
plugins:
  extref:
    enabled: true
```

```bash
SHOPANDA_PLUGINS_EXTREF_ENABLED=true
```

## Tests

```bash
go test ./plugins/extref/ -count=1
go test ./pkg/pluginsdk/harness/ -count=1
```

Copy patterns into product plugins; do not import this package from production code.