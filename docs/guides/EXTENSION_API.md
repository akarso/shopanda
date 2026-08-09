# Extension API (Stable v0 draft)

This document defines the first public stable surface for Shopanda plugin authors.

## Stable v0 (public)

Package: [`pkg/extapi`](../../pkg/extapi)

| Surface | Stable v0 includes |
| --- | --- |
| Hook point names | `extapi.HookCartAddItemBefore`, `extapi.HookCartAddItemAfter`, `extapi.HookCartUpdateItemBefore`, `extapi.HookCartRemoveItemAfter`, `extapi.HookCartRecalculateBefore`, `extapi.HookCartValidate`, `extapi.HookPoints()` |
| Slot anchor names | `extapi.SlotPDPInfo`, `extapi.SlotAnchors()`, `extapi.SlotAnchorNames()`, … |
| Slot placements | `extapi.PlacementBefore`, `PlacementAfter`, `PlacementPrepend`, `PlacementAppend` |
| Handler types | `extapi.HookHandler`, `extapi.SlotRenderer` with `HookContext` / `SlotRenderContext` |

Use these constants with `plugin.App` registration APIs:

```go
import (
    "github.com/akarso/shopanda/internal/platform/plugin"
    "github.com/akarso/shopanda/pkg/extapi"
)

func (p *Plugin) Init(app *plugin.App) error {
    if err := app.Hooks("acme/demo").Register(extapi.HookCartAddItemAfter, 100, func(ctx *extapi.HookContext) error {
        return nil
    }); err != nil {
        return err
    }
    return app.Slots("acme/demo").RegisterRenderer(extapi.SlotPDPInfo, extapi.PlacementAppend, 100, func(ctx *extapi.SlotRenderContext) string {
        return "<span>Eco badge</span>"
    })
}
```

`plugin.App` is compile-time wiring (internal); hook/slot names, placements, and handler types come from `pkg/extapi`.

## Internal (not stable)

Do not import these for compatibility-sensitive plugin code:

- `internal/application/*` implementation details beyond the narrow registration types shown above
- `internal/domain/*`
- `internal/infrastructure/*`

Internal packages may change without a deprecation window.

## Compatibility policy (v0)

### What is stable

- Hook point wire names (`extapi.HookPoints()`)
- Slot anchor wire names (`extapi.SlotAnchorNames()`)
- Placement wire values (`before`, `after`, `prepend`, `append`)
- Handler/context types in `pkg/extapi`

### Change rules

| Change type | Policy |
| --- | --- |
| Add hook point or slot anchor | Allowed in minor releases; document in release notes and `pkg/extapi` godoc |
| Rename or remove stable name | **Breaking** — requires deprecation notice for at least one minor release, migration note in release notes, and `pkg/extapi/compat_test.go` update |
| Change handler/context field semantics | **Breaking** — same deprecation window as renames |
| Internal registry or theme engine refactor | Allowed when `pkg/extapi` wire contracts stay compatible |

### Guard tests

`pkg/extapi/compat_test.go` fails CI when:

- stable hook/slot names drift from internal catalogs
- placement constants drift from internal slot placements
- stable anchor ordering diverges from `slots.StandardAnchors()`

Admin tooling (`GET /api/v1/admin/extensions/hooks`, `GET /api/v1/admin/extensions/slots`) uses the same canonical names as `pkg/extapi`.

### Dev diagnostics

When `SHOPANDA_DEV_MODE` is truthy (`1` / `true` / `yes`) and the storefront theme is enabled, registering a slot renderer for an anchor **not declared in the active theme** logs `slots.registration.unmarked_anchor`. Production behavior is unchanged (renderers for unmarked anchors are no-ops at render time).

## Theme inheritance (PR-712)

Child themes declare a parent in `theme.yaml`:

```yaml
name: my-child
version: "0.1.0"
parent: ../default
```

Child templates override parent templates by filename. Un overridden templates and layout are inherited from the parent chain. `parent` must be a relative path that stays within the theme boundary (parent directory of the loaded theme).
