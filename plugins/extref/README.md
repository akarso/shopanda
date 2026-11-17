# extref — public SDK reference plugin

Track E / follow-up (**PR-1551**, **PR-1553**) reference that uses **only** [`pkg/pluginsdk`](../../pkg/pluginsdk) and [`pkg/extapi`](../../pkg/extapi). Register with `pluginsdk.Adapt(extref.New())` in `cmd/api/register_plugins.go` (gated by `plugins.extref.enabled`).

## What it demonstrates

| Pattern | Where |
| --- | --- |
| `pluginsdk.Plugin` + `Needs()` (`CapCart`) | `plugin.go` |
| `RegisterField` — plugin-only `cart_item` fields | `extref.gift_note`, `extref.design_id` |
| `HookCartAddItemBefore` — reject blank `design_id` on write | `hooks.go` |
| `HookCartValidate` — empty stored / `pending_line` `design_id` | `hooks.go` |
| `Host.WithPluginWrite` + `CartPort.AddItem` | `cart_write.go` |
| Session cart + design-on-line route | `design_line.go` — `POST /extref/design-line` |
| `DiscardEmptyGuestCart` after failed first add | `design_line.go` |
| `RegisterPublicRoute` stub | `GET /extref/health` |

### Design-on-line (`POST /extref/design-line`)

1. **CSRF** — `/extref/` is on the same CSRF path list as `/storefront/` / `/checkout/` / `/account/`. Clients must send `X-CSRF-Token` (or the form token) on mutating methods. Copy that path prefix (or mount under `/storefront/`) in product plugins that set the cart cookie.
2. `EnsureStorefrontCart` — guest cookie via `pkg/storefrontsession` (no `cart_id` in JSON). Blank currency uses the store on the request context (same as storefront). Non-active / not-found / Forbidden (claimed) cookie carts are replaced; other `GetCart` errors do not rewrite the cookie. `Secure` comes from `storefrontsession.WithCookieSecure` (set by security middleware from trusted proxies), not from `r.TLS` alone.
3. Same `design_id` on the same variant → `200` `{"status":"unchanged"}` (quantity ignored).
4. Different `design_id` already on that variant → `409` `design_conflict` (`Access.ExclusiveString` + conditional upsert; `gift_note` stays updatable).
5. Variant line exists without design → `UpsertLineExtension` (`attached`) — does **not** increase quantity.
6. Otherwise trusted `AddItem` with `extref.design_id` (`cart.validate` sees values on `pending_line` before upsert).
7. Failed `AddItem` → `DiscardEmptyGuestCart` (empty guest cart + cookie cleanup).
8. Fail-closed: unbound `Cart()` → `503` (per request; CapCart is late-bound on serve).

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
