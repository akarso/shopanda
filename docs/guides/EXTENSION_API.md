# Extension API (Stable v0 draft)

This document defines the first public stable surface for Shopanda plugin authors.

## Stable v0 (public)

Package: [`pkg/extapi`](../../pkg/extapi)

| Surface | Stable v0 includes |
| --- | --- |
| Hook point names | `extapi.HookCartAddItemAfter`, `extapi.HookPoints()` |
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

## Compatibility policy (v0 draft)

- Stable v0 names/constants should remain wire-compatible within a major release.
- New hook points and slot anchors may be added without breaking existing plugins.
- Renames/removals require deprecation notes in release notes and `pkg/extapi` godoc.
- `pkg/extapi/compat_test.go` guards mapping to internal catalogs.

## Theme inheritance (PR-712)

Child themes declare a parent in `theme.yaml`:

```yaml
name: my-child
version: "0.1.0"
parent: ../default
```

Child templates override parent templates by filename. Un overridden templates and layout are inherited from the parent chain. `parent` must be a relative path that stays within the theme boundary (parent directory of the loaded theme).
