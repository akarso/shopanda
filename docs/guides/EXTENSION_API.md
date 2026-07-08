# Extension API (Stable v0 draft)

This document defines the first public stable surface for Shopanda plugin authors.

## Stable v0 (public)

Package: [`pkg/extapi`](../../pkg/extapi)

| Surface | Stable v0 includes |
| --- | --- |
| Hook point names | `extapi.HookCartAddItemAfter`, `extapi.HookPoints()` |
| Slot anchor names | `extapi.SlotPDPInfo`, `extapi.SlotAnchors()`, … |
| Slot placements | `extapi.PlacementBefore`, `PlacementAfter`, `PlacementPrepend`, `PlacementAppend` |

Use these constants with `plugin.App` registration APIs:

```go
import (
    "github.com/akarso/shopanda/internal/application/hooks"
    "github.com/akarso/shopanda/internal/application/slots"
    "github.com/akarso/shopanda/internal/platform/plugin"
    "github.com/akarso/shopanda/pkg/extapi"
)

func (p *Plugin) Init(app *plugin.App) error {
    if err := app.Hooks("acme/demo").Register(extapi.HookCartAddItemAfter, 100, func(ctx *hooks.Context) error {
        return nil
    }); err != nil {
        return err
    }
    return app.Slots("acme/demo").RegisterRenderer(extapi.SlotPDPInfo, slots.PlacementAppend, 100, func(ctx *slots.RenderContext) string {
        return "<span>Eco badge</span>"
    })
}
```

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

Child templates override parent templates by filename. Un overridden templates and layout are inherited from the parent chain.
