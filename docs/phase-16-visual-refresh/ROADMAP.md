# Phase 16 — Visual refresh roadmap

## Strategy

* Audit and decide before touching CSS (Track A). Direction choices are asked with a recommendation, never assumed.
* Foundation (tokens, vendored assets) before any page work on each surface.
* Admin (Track B) and storefront (Track C) are independent after PR-1301 and can run in parallel.
* Each page PR ships before/after screenshots and keeps markup hooks stable.
* PR specs live under `prs/` (**PR-1300–1330**). Each PR is **`[oss]`**.
* Individual PR specs at this stage carry Summary / Why / Scope / Out of scope / Decisions / Follow-up. Validation and documentation sections are written when the PR is implemented.

## Tracks

| Track | Goal | PR range | Delivers |
| --- | --- | --- | --- |
| **A** | Audit + direction | PR-1300–1301 | Baseline screenshots, findings, maintainer-approved `DESIGN_DIRECTION.md` |
| **B** | Admin | PR-1310–1315 | Vendored Pico + tokens, shell/nav, tables, forms, dashboard/feedback, a11y + dark mode |
| **C** | Storefront default theme | PR-1320–1326 | Tokens + fonts, header/footer, home/PLP, PDP, cart/checkout, account, a11y/responsive |
| **D** | Closeout | PR-1330 | After screenshots, theme token reference, product docs |

Recommended order: **A** first. Then **B 1310** and **C 1320** in parallel. Page PRs in each track in numeric order; a11y passes (1315, 1326) last per track. **D** last.

```text
1300 → 1301 ─┬─> 1310 → 1311 → 1312 → 1313 → 1314 → 1315 ─┐
             │                                              ├──> 1330
             └─> 1320 → 1321 → 1322 → 1323 → 1324 → 1325 → 1326 ─┘
```

## Coordination with Phase 15

Phase 15 adds admin pages and edits storefront header/footer. Avoid restyling the same markup twice:

| Phase 15 PR | Touches | Rule |
| --- | --- | --- |
| PR-1203, 1204, 1214, 1221, 1222 | New admin pages | Prefer landing **after 1310–1313** so they use the new form/table components. If they land first, 1312/1313 restyle them too. |
| PR-1223 | `_header.html`, logo/title/tagline | Land **before PR-1321**; 1321 styles the logo/tagline markup 1223 introduces. |
| PR-1231 | `_footer.html` identity + CMS links | Land **before PR-1321**, or 1321 restyles footer only and leaves structure to 1231. |

## Target outcome

When Phase 16 ships:

- Admin and storefront each have a documented token layer; no page uses literal colors
- Admin loads with **no CDN** requests (Pico, fonts, icons vendored)
- Admin lists, forms, settings, and dashboard share one set of components
- Default theme home, PLP, PDP, cart, checkout, and account follow one type scale and spacing rhythm
- Child themes can restyle by overriding a handful of documented variables in `custom.css`
- Both surfaces meet WCAG 2.2 AA for contrast, focus visibility, and keyboard use (per D9)
- Before/after screenshots exist for every touched route

## Track A — Audit + direction (PR-1300–1301)

| PR | Title | Short description |
| --- | --- | --- |
| PR-1300 | Design audit + baseline | Playwright baseline screenshots of admin and storefront routes; findings by severity; per-PR fix list. Docs only. |
| PR-1301 | Design direction | Put decisions D1–D9 to the maintainer with recommendations and rendered swatches; record answers in `specs/DESIGN_DIRECTION.md`. Docs only. |

## Track B — Admin (PR-1310–1315)

| PR | Title | Short description |
| --- | --- | --- |
| PR-1310 | Admin foundation | Vendor Pico v2; token layer mapped onto Pico vars; type scale; icon sprite (if D8). No layout change. |
| PR-1311 | Admin shell + navigation | Header, sidebar groups, active state, context switcher, mobile collapse. |
| PR-1312 | Admin lists + tables | Table density, toolbars/filters, pagination, status badges/chips, empty states. |
| PR-1313 | Admin forms + settings | Field groups, hints, scope badges/notes, validation states, save bar. |
| PR-1314 | Admin dashboard + feedback | Stat cards, alerts/toasts, loading states, confirm dialogs. |
| PR-1315 | Admin accessibility + dark mode | Contrast, focus rings, keyboard nav, landmarks; dark scheme (if D7). |

## Track C — Storefront default theme (PR-1320–1326)

| PR | Title | Short description |
| --- | --- | --- |
| PR-1320 | Storefront foundation | Two-layer tokens with legacy aliases; self-hosted font (if D4); replace literal values in `style.css`. |
| PR-1321 | Header, nav, footer | `_header.html`, `_nav.html`, `_footer.html`, account widget. |
| PR-1322 | Home + listing | `home.html`, `category.html`, `product_list.html`, product card. |
| PR-1323 | Product page | `product.html`: gallery, price block, variant/options, add-to-cart, details. |
| PR-1324 | Cart + checkout | `cart.html`, `checkout_*.html`: step indicator, summary, forms. |
| PR-1325 | Account area | `account_*.html`, `_account_nav.html`. |
| PR-1326 | Storefront accessibility + responsive | Contrast, focus, keyboard, 390px/900px breakpoints, `page.html`. |

## Track D — Closeout (PR-1330)

| PR | Title | Short description |
| --- | --- | --- |
| PR-1330 | Closeout | After screenshots vs PR-1300 baseline; token reference in THEME_SLOTS; MERCHANT/DEVELOPER/RUNBOOK notes. |

## Out of scope (phase)

| Item | Why |
| --- | --- |
| React / Tailwind / bundler for admin | Rewrite, not a refresh; AGENTS.md small-PR rule |
| Second shipped storefront theme | Later phase; this restyles `default` |
| Color / typography admin UI | Phase 15 non-goal stands; tokens live in CSS |
| Email templates / invoice PDF styling | Phase 15 Track B owns email layout |
| New backend data for layouts | Visual only |
| `examples/headless-vanilla` | Example, not product |

## Licensing

All PRs **`[oss]`** (GPL v3). Vendored assets must be GPL-compatible (Pico MIT, Lucide ISC, OFL fonts).
