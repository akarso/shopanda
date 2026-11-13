# Phase 16 — Visual refresh

Give the admin and the default storefront theme a deliberate, consistent look. Same stack (Pico + plain CSS + vanilla JS in admin; Go templates + htmx + plain CSS in `themes/default`). No new build pipeline, no framework swap.

## Why this phase

Phases 6–15 made the admin and storefront functionally complete. Visual quality never had its own track:

| Gap | Today | Impact |
| --- | --- | --- |
| Admin identity | Stock Pico v2 from a CDN + ~480 lines of overrides | Looks like a default template; CDN is a runtime dependency |
| Admin consistency | ~12k lines of `admin.js` render HTML ad hoc (`settings-scope-note`, `stat-card`, `chip`, `badge`, inline styles) | Same concept looks different page to page |
| Admin density | Pico defaults tuned for content sites | Tables and forms waste space; long settings pages |
| Storefront tokens | `style.css` has ~14 tokens but ~1,100 lines with repeated font stacks and literal values | Child themes cannot restyle by overriding a few variables |
| Storefront polish | Header/PLP/PDP/checkout styled once, never reviewed as a whole | Uneven hierarchy, spacing, and responsive behavior |
| Accessibility | Never audited | Contrast, focus, and keyboard gaps unknown |

## Design principles

- **Ask, then build.** Every visual direction choice is put to the maintainer with a recommendation (PR-1301). Nothing aesthetic is decided silently inside an implementation PR.
- **Tokens first.** Each surface gets a token layer (color roles, type scale, spacing, radius, shadow) before any page is restyled. Pages consume tokens; no new literal colors.
- **Same stack.** Plain CSS, no preprocessor, no npm. Admin stays an embedded SPA (`go:embed dist/*`). Storefront stays theme files on disk.
- **No CDN at runtime.** Pico, fonts, and icons are vendored and served from the binary / theme `static/`.
- **Markup contracts stay stable.** `data-*` hooks, IDs, and classes used by `admin.js`, the `*_test.js` suites, Go handler tests, and child themes are kept or migrated in the same PR.
- **Child themes keep working.** `themes/_starter` and any `parent: ../default` theme must render without edits; new tokens are additive.
- **One surface area per PR.** Reviewable in ~10–20 minutes with before/after screenshots.

See [`ROADMAP.md`](ROADMAP.md), [`specs/VISUAL_REFRESH.md`](specs/VISUAL_REFRESH.md), and [`prs/`](prs/).

## Status

**Planned.** PR range **PR-1300–1330**.

## Relationship

| Phase | Focus | Status |
| --- | --- | --- |
| Phase 6 | Merchant-complete admin SPA | Shipped |
| Phase 7 Track F / 14 | Theme inheritance, slots, `theme:init`, headless | Shipped |
| Phase 15 | Store identity, email templates admin, Appearance (themes, logo, tagline) | Planned — see coordination in ROADMAP |
| **Phase 16** | Visual refresh: admin + default theme | **Planned** |
