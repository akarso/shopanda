# Theme Author Guide — Slots & Inheritance

How to customize the Shopanda storefront theme while keeping **plugin injection points** intact.

Audience: theme authors, integrators  
Status: guide (Track F, PR-711–715)

Related:

- [Plugin Authoring Guide](../../PLUGINS.md) — slot registration from the plugin side
- [Extension API (Stable v0)](EXTENSION_API.md) — anchor constants in `pkg/extapi`
- [Multi-Plugin Composition](PLUGIN_COMPOSITION.md) — combining multiple plugin renderers
- [Theme system spec](../phase-1-core/specs/THEME_SYSTEM.md) — SSR architecture overview
- Reference plugin: [plugins/slotsdemo](../../plugins/slotsdemo/README.md)

---

## Core rule

**Themes own structure; plugins inject.**

Plugins register HTML renderers against **named anchors** declared in your templates. If you remove or rename an anchor, plugin output is silently skipped — there is no auto-injection or layout merge tree (no Magento `layout.xml`).

When you customize markup:

- preserve standard anchor names, or document replacements for plugin authors
- keep `{{slot_container}}` / `{{slot}}` markers in the same logical region (header, PDP info column, cart summary, etc.)
- put global CSS/JS in the **plugin asset manifest**, not slot renderers

---

## Theme layout

A theme is a directory pointed to by `frontend.theme_path` (default: `themes/default`).

```text
themes/my-store/
  theme.yaml
  templates/
    layout.html          # page shell (required)
    _header.html         # layout partial (optional)
    _nav.html
    _footer.html
    product.html         # page templates
    cart.html
    ...
  static/
    css/
    js/
```

### `theme.yaml`

Minimum metadata:

```yaml
name: my-store
version: "0.1.0"
```

Optional **parent** for inheritance (see below):

```yaml
name: my-store
version: "0.1.0"
parent: ../default
```

Storefront URLs and nav items can also live under `storefront:` — see `themes/default/theme.yaml`.

---

## Theme inheritance

Child themes extend a parent without copying every template.

| Rule | Behavior |
| --- | --- |
| `parent:` | Relative path from child theme root (e.g. `../default`) |
| Override | Child file with the **same filename** wins |
| Inherit | Parent templates used when child has no matching file |
| Partials | Same child-wins rule for `_*.html` partials |
| Boundary | Parent must resolve inside the themes tree (no absolute paths) |

### Example: footer-only override

```text
themes/
  default/          # parent — full template set + slot markers
  my-store/
    theme.yaml        # parent: ../default
    templates/
      _footer.html    # only override footer; header, nav, pages inherited
```

The engine merges the inheritance chain at load time. Parent slot markers in non-overridden templates remain available to plugins.

Test fixtures: `internal/domain/theme/testdata/child_partial_footer/` (child replaces `_footer.html` only).

### When to use inheritance

| Goal | Approach |
| --- | --- |
| Tweak footer legal links | Child `_footer.html` only |
| New PDP layout | Child `product.html` — **keep `pdp.*` anchors** |
| Full rebrand | Child theme with `parent: ../default`; override partials incrementally |
| Unrelated greenfield theme | New theme directory; declare your own anchors and document them |

---

## Layout partials

`layout.html` is the HTML shell. Large regions are split into **`_*.html` partials** so you can override one region without forking the whole layout.

Default theme split:

| Partial | Contains | Key anchors |
| --- | --- | --- |
| `layout.html` | `<head>`, `<body>`, main wrapper | `layout.head`, `layout.body_start`, `layout.main`, `layout.body_end` |
| `_header.html` | Brand bar, search, account/cart widgets | `layout.header` (container) |
| `_nav.html` | Primary + category navigation | `layout.nav`, `layout.category_nav` |
| `_footer.html` | Site footer | `layout.footer` (container) |

`layout.html` includes partials with:

```html
{{ template "_header.html" . }}
...
{{ template "_footer.html" . }}
```

**Convention:** files named `_something.html` are partials — registered in the layout template set, not as standalone page routes.

---

## Slot markers

Two template forms declare anchors.

### `slot_container` (recommended)

Wrap a block of markup. The engine expands it to `before` / `prepend` / `append` / `after` placements automatically.

```html
{{slot_container "layout.footer"}}
<footer class="site-footer">
    ...
</footer>
{{/slot_container}}
```

When the inner content is a **single root element**, placements attach around that element (see `_nav.html` in the default theme for explicit `slot` usage on the same anchor).

### Explicit `slot`

Fine-grained control inside a region:

```html
{{slot . "layout.nav" "before"}}
<nav>...</nav>
{{slot . "layout.nav" "after"}}
```

Placements (same for all anchors):

| Placement | Position |
| --- | --- |
| `before` | Immediately before the container / element |
| `prepend` | Inside container, before theme content |
| `append` | Inside container, after theme content |
| `after` | Immediately after the container / element |

Plugins choose a placement when registering (`extapi.PlacementAppend`, etc.). Multiple plugins on the same anchor+placement run in **priority** order (lower number first).

### Nested `slot_container`

Nested blocks are supported — the preprocessor matches opening/closing pairs by depth (inner containers expand first):

```html
{{slot_container "pdp.info"}}
<section class="pdp-info">
  {{slot_container "pdp.actions"}}
  <div class="pdp-actions">...</div>
  {{/slot_container}}
</section>
{{/slot_container}}
```

For fine-grained control inside a single region, explicit `{{slot . "anchor" "placement"}}` markers still work and compose with containers.

---

## Standard anchor catalog

Canonical names live in `internal/application/slots/catalog.go` (`slots.StandardAnchors()`). Custom themes should **preserve these names** unless you intentionally fork the plugin ecosystem.

| Anchor | Page / region | Typical use |
| --- | --- | --- |
| `layout.head` | `<head>` end | Meta tags, inline snippets |
| `layout.body_start` | Start of `<body>` | Body-open widgets |
| `layout.header` | Site header | Announcement bar, trust badges |
| `layout.nav` | Primary nav | Extra nav links |
| `layout.category_nav` | Category nav | Category promos |
| `layout.main` | Main content wrapper | Page-level banners |
| `layout.footer` | Site footer | Legal, payment icons |
| `layout.body_end` | End of `<body>` | Deferred scripts (prefer asset manifest) |
| `home.hero` | Home hero | Campaign hero |
| `pdp.gallery` | PDP media | Badges on image |
| `pdp.info` | PDP info column | Attributes, eco labels |
| `pdp.actions` | PDP add-to-cart | Extra buttons, financing |
| `plp.toolbar` | Category / PLP | Sort/filter plugins |
| `cart.items` | Cart line table | Per-line upsells |
| `cart.summary` | Cart aside | Shipping estimator |
| `checkout.progress` | Checkout steps | Step indicators |
| `checkout.panel` | Checkout form | Extra fields |
| `checkout.summary` | Checkout aside | Order summary widgets |
| `account.nav` | Signed-in account | Section nav extras |

**Default theme files to copy from:** `themes/default/templates/` — especially `layout.html`, `_header.html`, `_nav.html`, `_footer.html`, `product.html`, `cart.html`, `home.html`, `checkout_*.html`, `_account_nav.html`.

Stable constants for plugin authors: `pkg/extapi` (`extapi.SlotLayoutFooter`, `extapi.SlotPDPInfo`, …).

---

## Coordinating with plugins

### Preserve anchors when redesigning

If you change PDP markup but keep the same anchors, existing plugins keep working:

```html
{{slot_container "pdp.info"}}
<div class="my-pdp-info">
    <!-- your structure -->
    {{slot . "pdp.actions" "append"}}
</div>
{{/slot_container}}
```

If you rename `pdp.info` → `product.sidebar`, plugins registering on `pdp.info` will not render until plugin authors update — treat renames as a **breaking theme change**.

### CSS and JavaScript

| Need | Use |
| --- | --- |
| Plugin-owned global assets | `app.Assets().RegisterManifest()` — route-gated CSS/JS in layout head/footer |
| Small HTML fragment | Slot renderer |
| Theme-owned styling | `static/css/` in your theme |

### Verify during development

With dev mode enabled, the slot registry warns when a plugin registers an anchor **not declared** in the active theme (`slots.registration.unmarked_anchor`).

Inspect registrations at runtime:

- `GET /api/v1/admin/extensions/slots` (requires `extensions.read`)

Response includes the standard catalog plus registered handlers per anchor.

---

## Checklist for a new child theme

1. Create `themes/my-store/theme.yaml` with `parent: ../default` (or your base).
2. Set `frontend.theme_path: themes/my-store` in config.
3. Override only the templates/partials you need (`_footer.html`, `product.html`, …).
4. In every overridden file, **keep or intentionally replace** slot markers from the parent.
5. Run the API in dev mode and enable a reference plugin (`plugins.slotsdemo.enabled: true`) to confirm DOM injection.
6. Compare declared anchors: inherited theme scan uses the same rules as production (`themeapp.DeclaredAnchorsFromDir`).

---

## What is out of scope

| Approach | Status |
| --- | --- |
| Magento-style `layout.xml` merge | Not supported |
| Plugin-driven DOM reordering | Not supported — themes own structure |
| `layout.yaml` block reorder (PR-716) | Stretch / not shipped yet |
| Strict CI validation of unknown anchors (PR-720) | Stretch / not shipped yet |
| Nested `slot_container` fix (PR-717) | Shipped — depth-aware preprocessor |

---

## Further reading

- Plugin slot registration: [PLUGINS.md § UI slots](../../PLUGINS.md)
- Stable v0 contracts: [EXTENSION_API.md](EXTENSION_API.md)
- Phase 7 Track F roadmap: [ROADMAP.md](../phase-7-customization-platform/ROADMAP.md)
