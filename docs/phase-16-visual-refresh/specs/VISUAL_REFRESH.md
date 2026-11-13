# Visual refresh — specification

Working rules for Phase 16. The concrete direction (palette, type, density) is **not** in this file; it is recorded in `DESIGN_DIRECTION.md`, written by PR-1301 after the maintainer answers the decision list below.

## 1. Surfaces

| Surface | Files | Stack | Constraints |
| --- | --- | --- | --- |
| Admin SPA | `internal/interfaces/http/admin/dist/` (`index.html`, `admin.css`, `admin.js`, `*_section.js`, `*_admin_ui.js`) | Pico v2 + plain CSS + vanilla JS, embedded via `go:embed` | Node `*_test.js` suites and `admin_handler_test.go` assert markup; keep hooks stable |
| Default storefront theme | `themes/default/templates/*.html`, `themes/default/static/css/style.css`, `custom.css` | Go `html/template` + htmx | CSP `script-src 'self' 'nonce-…'`; full-page cache; child themes inherit partials and CSS |
| Starter theme | `themes/_starter/` | `parent: ../default` + `custom.css` | Must keep rendering unchanged except for intended token effects |

Out of surface: transactional emails (Phase 15 Track B owns `_layout.html`), `examples/headless-vanilla`, invoice PDFs.

## 2. Decision list (PR-1301)

Each item is asked with options and a recommendation. Answers are recorded in `DESIGN_DIRECTION.md` with date and rationale.

| # | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| D1 | Admin personality | Neutral tool (Linear/Stripe-like) · Warm, matches storefront · Branded/bold | Neutral tool — admin is a work surface; storefront carries the brand |
| D2 | Storefront personality | Refine current warm editorial look · New direction | Refine current — it already has a point of view; fix execution, not identity |
| D3 | Admin base CSS | Keep Pico v2, vendor it · Drop Pico for own base layer | Keep + vendor; revisit only if overrides fight Pico |
| D4 | Typography | System stacks only · Self-hosted fonts (one per surface) | Admin: system UI stack. Storefront: one self-hosted display face + system body |
| D5 | Accent color | 3 candidate palettes per surface shown as swatches + contrast ratios | Decide at PR-1301 from rendered swatches |
| D6 | Admin density | Comfortable (Pico default) · Compact tables/forms | Compact for tables, comfortable for forms |
| D7 | Dark mode | None · Admin only (`prefers-color-scheme` + toggle) · Both | Admin only; storefront dark mode is a theme concern |
| D8 | Icons | None · Vendored SVG sprite (e.g. Lucide subset, ISC) | Small vendored sprite for nav and status only |
| D9 | Accessibility target | Best effort · WCAG 2.2 AA | WCAG 2.2 AA for contrast, focus, keyboard |

Implementation PRs that hit an unlisted aesthetic choice stop and ask; the answer is appended to `DESIGN_DIRECTION.md`.

## 3. Token model

Two layers per surface, plain CSS custom properties:

```text
primitives   --color-neutral-50 … --color-neutral-950, --color-accent-*, --space-1 … --space-8, --font-size-*
semantic     --surface, --surface-raised, --text, --text-muted, --border, --accent, --accent-contrast,
             --danger, --warning, --success, --focus-ring, --radius-*, --shadow-*
```

- Admin: semantic tokens map onto Pico variables (`--pico-primary`, `--pico-background-color`, …) so Pico components inherit them.
- Storefront: existing names (`--bg`, `--panel`, `--text`, `--muted`, `--accent`, …) are kept as aliases for one phase so child themes overriding them keep working.
- Pages and components consume semantic tokens only.

## 4. Asset policy

- Pico, fonts, icons: vendored with license file next to the asset. No `https://` stylesheet or font links.
- Fonts: `woff2`, `font-display: swap`, at most two weights per family.
- Total added CSS per surface stays small; prefer deleting overrides over adding.

## 5. Skill workflow

| Step | Skill | Used in |
| --- | --- | --- |
| Audit | `design-audit` | PR-1300 |
| Baseline + after screenshots | `webapp-testing` (Playwright) | PR-1300, every visual PR, PR-1330 |
| Direction (asks maintainer) | `bencium-controlled-ux-designer` | PR-1301, any new open choice |
| Tokens | `design-system` | PR-1310, PR-1320 |
| Type details | `typography` | PR-1310, PR-1320, page PRs |
| Accessibility / UX review | `web-design-guidelines` | PR-1315, PR-1326, spot checks |

Skills not used: React/Next/Tailwind/shadcn skills (wrong stack), banner/slides/brand-guidelines (off-topic), `frontend-design` / `bencium-impact-designer` (decide without asking — conflicts with D-list policy).

## 6. Verification

Every visual PR:

1. Before/after screenshots at 1440px and 390px for each touched route.
2. `go test ./internal/interfaces/http/admin/... ./internal/interfaces/http/storefront/... ./internal/domain/theme/... ./internal/infrastructure/themefs/...`
3. Admin JS suites (`*_test.js`) run through `go test ./internal/interfaces/http/admin/...` (needs `node` on PATH) — must pass when admin markup changes.
4. `_starter` theme renders (child-theme regression).

## 7. Non-goals

- New admin framework, bundler, TypeScript, Tailwind
- New storefront theme (this restyles `default`; a second shipped theme is a later phase)
- Admin UI for colors / typography (Phase 15 non-goal stands)
- Layout changes that require new backend data or APIs
- Restyling transactional emails or invoice PDFs
